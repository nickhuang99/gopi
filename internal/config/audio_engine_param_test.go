package config

// audio_engine_param_test.go — the regression guard for the audio configuration
// surface (2026-10-09): the nested blocks, the legacy fold, the ONE precedence,
// and the credential questions `gopi config show` and the 0600 write guard ask
// of a nested document.
//
// Each test states the failure it catches; none of them asserts a shape that a
// refactor "already looked right" could satisfy.

import (
	"encoding/json"
	"strings"
	"testing"
)

// writeSettings (config_test.go) writes a settings document and returns its
// path — reused rather than re-declared (Rule 14).

// TestAudioEngineParamNestedBlocksAreRead is the base case the rest build on: a
// document written the CURRENT way resolves every knob, and the engine selector
// with it. A failure here means the nested surface does not load at all.
func TestAudioEngineParamNestedBlocksAreRead(t *testing.T) {
	path := writeSettings(t, `{
	  "audio_engine": "streaming",
	  "audio_engine_param": {
	    "recording": { "api_key": "k-rec", "key_file": "/tmp/k", "api_base_url": "https://h/root",
	                   "resource_id": "volc.x", "hotwords": "a,b" },
	    "streaming": { "app_id": "app-1", "token": "tok-1", "endpoint": "wss://h/asr", "cluster": "c1" },
	    "sauc":      { "app_id": "app-2", "token": "tok-2", "endpoint": "wss://h/sauc",
	                   "resource_id": "volc.seedasr.sauc.duration" },
	    "local":     { "models_dir": "/m", "runtime_bin": "/r" }
	  }
	}`)
	cfg, err := FromSettingsFile(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.AudioEngine != AudioEngineStreaming {
		t.Errorf("AudioEngine = %q, want %q", cfg.AudioEngine, AudioEngineStreaming)
	}
	rec := cfg.AudioEngineParam.Recording
	if rec.APIKey != "k-rec" || rec.KeyFile != "/tmp/k" || rec.APIBaseURL != "https://h/root" {
		t.Errorf("recording block = %+v", rec)
	}
	if rec.ResourceID != "volc.x" || rec.Hotwords != "a,b" {
		t.Errorf("recording block (resource/hotwords) = %+v", rec)
	}
	stream := cfg.AudioEngineParam.Streaming
	if stream.AppID != "app-1" || stream.Token != "tok-1" || stream.Endpoint != "wss://h/asr" || stream.Cluster != "c1" {
		t.Errorf("streaming block = %+v", stream)
	}
	// The v3 block carries the SAME credential pair as the streaming block
	// (2026-10-14): the vendor issues one application id and one token, so the
	// two blocks spell them the same way and this assertion is what keeps a
	// renamed knob from silently landing in only one of them.
	sauc := cfg.AudioEngineParam.Sauc
	if sauc.AppID != "app-2" || sauc.Token != "tok-2" {
		t.Errorf("sauc block credential = %+v", sauc)
	}
	if sauc.Endpoint != "wss://h/sauc" || sauc.ResourceID != "volc.seedasr.sauc.duration" {
		t.Errorf("sauc block transport = %+v", sauc)
	}
	loc := cfg.AudioEngineParam.Local
	if loc.ModelsDir != "/m" || loc.RuntimeBin != "/r" {
		t.Errorf("local block = %+v", loc)
	}
}

// TestAudioEngineLegacyKeysFoldIntoTheNestedBlocks is the migration contract:
// a document nobody has edited keeps working, knob by knob, and the ENGINE NAME
// it spelled is translated rather than rejected.
func TestAudioEngineLegacyKeysFoldIntoTheNestedBlocks(t *testing.T) {
	path := writeSettings(t, `{
	  "voice_engine": "doubao",
	  "doubao_api_key": "legacy-key",
	  "doubao_key_file": "/legacy/k",
	  "doubao_base_url": "https://legacy/root",
	  "doubao_resource_id": "legacy.rid",
	  "doubao_hotwords": "l1,l2",
	  "doubao_app_id": "legacy-app",
	  "doubao_token": "legacy-tok",
	  "doubao_stream_endpoint": "wss://legacy/asr"
	}`)
	cfg, err := FromSettingsFile(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.AudioEngine != AudioEngineRecording {
		t.Errorf("a legacy engine NAME %q must translate to %q, got %q",
			AudioEngineLegacyVendorRecording, AudioEngineRecording, cfg.AudioEngine)
	}
	rec := cfg.AudioEngineParam.Recording
	if rec.APIKey != "legacy-key" || rec.KeyFile != "/legacy/k" || rec.APIBaseURL != "https://legacy/root" ||
		rec.ResourceID != "legacy.rid" || rec.Hotwords != "l1,l2" {
		t.Errorf("legacy keys did not fold into the recording block: %+v", rec)
	}
	stream := cfg.AudioEngineParam.Streaming
	if stream.AppID != "legacy-app" || stream.Token != "legacy-tok" || stream.Endpoint != "wss://legacy/asr" {
		t.Errorf("legacy keys did not fold into the streaming block: %+v", stream)
	}
	// The KEY FILE is ONE legacy key feeding TWO blocks, and the fold must give
	// both of them the answer — a document that named it expects BOTH engines
	// to find their credential in it.
	if stream.KeyFile != "/legacy/k" {
		t.Errorf("streaming.key_file = %q, want the legacy key file to feed it too", stream.KeyFile)
	}
	// The raw document value is preserved, because the notice has to report what
	// the OPERATOR wrote (a folded value can never say "doubao").
	if cfg.LegacyVoiceEngine != AudioEngineLegacyVendorRecording {
		t.Errorf("LegacyVoiceEngine = %q, want the raw document value %q",
			cfg.LegacyVoiceEngine, AudioEngineLegacyVendorRecording)
	}
}

// TestAudioNestedBlockWinsOverTheLegacyKey is the precedence, and it is tested
// FIELD BY FIELD rather than document by document: an operator mid-migration has
// some knobs on each surface, and "the new surface wins outright" would silently
// discard the ones they had not moved yet.
func TestAudioNestedBlockWinsOverTheLegacyKey(t *testing.T) {
	path := writeSettings(t, `{
	  "doubao_api_key": "OLD",
	  "doubao_hotwords": "OLD-HW",
	  "audio_engine_param": { "recording": { "api_key": "NEW" } }
	}`)
	cfg, err := FromSettingsFile(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := cfg.AudioEngineParam.Recording.APIKey; got != "NEW" {
		t.Errorf("api_key = %q, want the nested value to win where both are present", got)
	}
	if got := cfg.AudioEngineParam.Recording.Hotwords; got != "OLD-HW" {
		t.Errorf("hotwords = %q, want the legacy value to survive where the nested one is silent", got)
	}
}

// TestAudioEngineUnknownBlocksAndKnobsAreNamed is Rule 15 on this surface: a
// typo must be REPORTED, with what is accepted there, never silently ignored.
// The failure this catches is the default behaviour of encoding/json.
func TestAudioEngineUnknownBlocksAndKnobsAreNamed(t *testing.T) {
	raw := map[string]json.RawMessage{
		AudioEngineParamSettingsKey: json.RawMessage(`{
		  "recording": { "api_ky": "typo", "hotwords": "ok" },
		  "batch": { "x": 1 }
		}`),
	}
	_, unknown, err := DecodeAudioEngineParam(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	paths := make([]string, 0, len(unknown))
	for _, u := range unknown {
		paths = append(paths, u.Path)
	}
	// Sorted report order: a block before the knobs inside other blocks, both
	// deterministic. Asserting the SET rather than one element is the point —
	// an unknown BLOCK and an unknown KNOB are two different mistakes and both
	// must be named.
	for _, want := range []string{"batch", "recording.api_ky"} {
		if !containsString(paths, want) {
			t.Errorf("unknown set %v does not name %q", paths, want)
		}
	}
	if containsString(paths, "recording.hotwords") {
		t.Errorf("a KNOWN knob was reported as unknown: %v", paths)
	}
	err = AudioEngineUnknownError(unknown)
	if err == nil {
		t.Fatal("AudioEngineUnknownError returned nil for a non-empty unknown list")
	}
	// The message names what IS accepted — a report that only says "wrong" makes
	// the operator guess.
	if !strings.Contains(err.Error(), AudioParamKeyAPIKey) {
		t.Errorf("the refusal does not name the accepted knob %q: %v", AudioParamKeyAPIKey, err)
	}
}

// TestAudioEngineUnknownAbsentIsClean: the report must be EMPTY (no error, no
// line) on a document that is correct — otherwise the notice becomes noise and
// the reader learns to ignore it.
func TestAudioEngineUnknownAbsentIsClean(t *testing.T) {
	raw := map[string]json.RawMessage{
		AudioEngineParamSettingsKey: json.RawMessage(`{"local": {"models_dir": "/m"}}`),
	}
	_, unknown, err := DecodeAudioEngineParam(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(unknown) != 0 {
		t.Errorf("unknown = %v, want none", unknown)
	}
	if err := AudioEngineUnknownError(unknown); err != nil {
		t.Errorf("AudioEngineUnknownError = %v, want nil", err)
	}
}

// TestAudioEngineParamMalformedIsAnError: `audio_engine_param` that is not an
// object says something about the engine parameters and cannot be read. It must
// not resolve to "nothing was configured" — that is a run against a default the
// operator did not choose, with no message.
func TestAudioEngineParamMalformedIsAnError(t *testing.T) {
	raw := map[string]json.RawMessage{
		AudioEngineParamSettingsKey: json.RawMessage(`"not-an-object"`),
	}
	if _, _, err := DecodeAudioEngineParam(raw); err == nil {
		t.Fatal("a malformed audio_engine_param loaded silently")
	}
	// And it must reach the LOADER, not only the decoder.
	path := writeSettings(t, `{"audio_engine_param": [1,2,3]}`)
	if _, err := FromSettingsFile(path); err == nil {
		t.Fatal("FromSettingsFile accepted a malformed audio_engine_param")
	}
}

// TestAudioCredentialParamNameCoversNestedLeaves is the redaction contract
// `gopi config show` depends on: the renderer asks about a DOTTED PATH, so the
// predicate must answer for the path and for the bare leaf, and must say NO
// about the knobs that are not credentials.
func TestAudioCredentialParamNameCoversNestedLeaves(t *testing.T) {
	credential := []string{
		AudioParamSettingsPath(AudioEngineRecording, AudioParamKeyAPIKey),
		AudioParamSettingsPath(AudioEngineStreaming, AudioParamKeyAppID),
		AudioParamSettingsPath(AudioEngineStreaming, AudioParamKeyToken),
		// The v3 block's pair (2026-10-14): the SAME leaf names, a different
		// block. Without these two rows an inline v3 credential would be
		// printed by `gopi config show` while the v2 half of the same pair was
		// redacted — the exact drift the leaf-name list exists to make
		// impossible.
		AudioParamSettingsPath(AudioEngineSauc, AudioParamKeyAppID),
		AudioParamSettingsPath(AudioEngineSauc, AudioParamKeyToken),
		// The bare leaf, for a caller that already split the path.
		AudioParamKeyAPIKey,
	}
	for _, k := range credential {
		if !IsAudioCredentialParamName(k) {
			t.Errorf("IsAudioCredentialParamName(%q) = false, want true", k)
		}
	}
	// The FALSE half is the load-bearing one: a predicate that said "true" for
	// every nested path would redact `endpoint`, `models_dir` and `hotwords`,
	// and an operator could no longer read their own configuration.
	open := []string{
		AudioParamSettingsPath(AudioEngineStreaming, AudioParamKeyEndpoint),
		AudioParamSettingsPath(AudioEngineRecording, AudioParamKeyHotwords),
		AudioParamSettingsPath(AudioEngineRecording, AudioParamKeyAPIBaseURL),
		AudioParamSettingsPath(AudioEngineLocal, AudioParamKeyModelsDir),
		AudioParamSettingsPath(AudioEngineLocal, AudioParamKeyRuntimeBin),
		AudioParamSettingsPath(AudioEngineStreaming, AudioParamKeyCluster),
		// The v3 block's TRANSPORT knobs are not credentials, in that block as
		// in this one.
		AudioParamSettingsPath(AudioEngineSauc, AudioParamKeyEndpoint),
		AudioParamSettingsPath(AudioEngineSauc, AudioParamKeyResourceID),
	}
	for _, k := range open {
		if IsAudioCredentialParamName(k) {
			t.Errorf("IsAudioCredentialParamName(%q) = true — a non-credential would be redacted", k)
		}
	}
}

// TestSettingsDocumentCarriesNestedCredential is the 0600 guard's input: since
// the rename a credential can live INSIDE the nested block, and a guard that
// only looked at the top level would leave a live key world-readable.
func TestSettingsDocumentCarriesNestedCredential(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"nested api_key", `{"audio_engine_param":{"recording":{"api_key":"live"}}}`, true},
		{"nested app_id", `{"audio_engine_param":{"streaming":{"app_id":"app"}}}`, true},
		{"nested token", `{"audio_engine_param":{"streaming":{"token":"tok"}}}`, true},
		{"nested but empty", `{"audio_engine_param":{"recording":{"api_key":""}}}`, false},
		{"nested sauc app_id", `{"audio_engine_param":{"sauc":{"app_id":"app"}}}`, true},
		{"nested sauc token", `{"audio_engine_param":{"sauc":{"token":"tok"}}}`, true},
		{"nested sauc pair empty", `{"audio_engine_param":{"sauc":{"app_id":"","token":""}}}`, false},
		{"top-level legacy", `{"doubao_api_key":"live"}`, true},
		{"no credential at all", `{"audio_engine_param":{"local":{"models_dir":"/m"}}}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSettings(t, tc.body)
			carries, readable := SettingsDocumentCarriesCredential(path)
			if !readable {
				t.Fatalf("document reported unreadable: %s", tc.body)
			}
			if carries != tc.want {
				t.Errorf("SettingsDocumentCarriesCredential = %v, want %v for %s", carries, tc.want, tc.body)
			}
		})
	}
}

// TestAudioLegacyNotesAreSilentWhenMigrated: the notice is for an operator who
// has not migrated. A migrated document and a clean environment must produce
// NO lines, or the reader learns that the line is background noise and stops
// seeing it on the host that needs it.
func TestAudioLegacyNotesAreSilentWhenMigrated(t *testing.T) {
	path := writeSettings(t, `{"audio_engine":"recording",
	  "audio_engine_param":{"recording":{"api_key":"k"}}}`)
	cfg, err := FromSettingsFile(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if note := cfg.AudioLegacyKeyNote(); note != "" {
		t.Errorf("AudioLegacyKeyNote on a migrated document = %q, want empty", note)
	}
	if note := cfg.AudioEngineUnknownNote(); note != "" {
		t.Errorf("AudioEngineUnknownNote on a clean document = %q, want empty", note)
	}
}

// TestAudioLegacyKeyNoteNamesTheReplacement: when the notice DOES fire it must
// name the destination, not only the problem. A notice that says "this key is
// old" without saying where the value goes leaves the operator to read the
// source.
func TestAudioLegacyKeyNoteNamesTheReplacement(t *testing.T) {
	path := writeSettings(t, `{"doubao_token":"live"}`)
	cfg, err := FromSettingsFile(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	note := cfg.AudioLegacyKeyNote()
	if note == "" {
		t.Fatal("AudioLegacyKeyNote on a legacy document = empty, want a notice")
	}
	wantPath := AudioParamSettingsPath(AudioEngineStreaming, AudioParamKeyToken)
	if !strings.Contains(note, wantPath) {
		t.Errorf("the notice %q does not name the replacement path %q", note, wantPath)
	}
}

// TestResolveAudioLocalReadsTheEnvironment is this engine's FIRST env channel
// (before 2026-10-09 there was no settings or env surface for the local
// deployment at all). The failure it catches is a resolver that reads the
// settings block and forgets the environment above it.
func TestResolveAudioLocalReadsTheEnvironment(t *testing.T) {
	t.Setenv(AudioLocalModelsEnv, "/env/models")
	t.Setenv(AudioLocalRuntimeEnv, "/env/runtime")
	path := writeSettings(t, `{"audio_engine_param":{"local":{"models_dir":"/settings/models"}}}`)
	cfg, err := FromSettingsFile(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	got := ResolveAudioLocal(cfg)
	if got.ModelsDir != "/env/models" {
		t.Errorf("models_dir = %q, want the environment to outrank the settings block", got.ModelsDir)
	}
	if got.RuntimeBin != "/env/runtime" {
		t.Errorf("runtime_bin = %q, want the environment value", got.RuntimeBin)
	}
}

// TestResolveAudioRecordingPrefersTheCurrentEnvNameOverThePreRenameOne: a
// script that exports BOTH names is explicit twice, and the answer must be the
// name the surface currently publishes.
func TestResolveAudioRecordingPrefersTheCurrentEnvNameOverThePreRenameOne(t *testing.T) {
	t.Setenv(AudioHotwordsEnv, "new-name")
	t.Setenv(DoubaoHotwordsEnv, "old-name")
	got := ResolveAudioRecording(nil)
	if got.Hotwords != "new-name" {
		t.Errorf("hotwords = %q, want the CURRENT env name to win over the pre-rename one", got.Hotwords)
	}
	// And with only the old name set, the old name answers — the migration is
	// not a breaking change.
	t.Setenv(AudioHotwordsEnv, "")
	t.Setenv(DoubaoHotwordsEnv, "only-old")
	got = ResolveAudioRecording(nil)
	if got.Hotwords != "only-old" {
		t.Errorf("hotwords = %q, want the pre-rename env name to still answer", got.Hotwords)
	}
}

// TestAudioEngineLegacyVendorNameIsTheOneMapping pins the translation table
// itself: every caller (resolver, validator, provenance line) asks THIS
// function, so a second opinion cannot exist — and a value that is already
// current must pass through untouched rather than being reported as legacy.
func TestAudioEngineLegacyVendorNameIsTheOneMapping(t *testing.T) {
	cases := []struct {
		in       string
		want     string
		wasThere bool
	}{
		{AudioEngineLegacyVendorRecording, AudioEngineRecording, true},
		{AudioEngineLegacyVendorStreaming, AudioEngineStreaming, true},
		{AudioEngineRecording, AudioEngineRecording, false},
		{AudioEngineStreaming, AudioEngineStreaming, false},
		{AudioEngineLocal, AudioEngineLocal, false},
		{"nonsense", "nonsense", false},
	}
	for _, tc := range cases {
		got, wasLegacy := AudioEngineLegacyVendorName(tc.in)
		if got != tc.want || wasLegacy != tc.wasThere {
			t.Errorf("AudioEngineLegacyVendorName(%q) = (%q, %v), want (%q, %v)",
				tc.in, got, wasLegacy, tc.want, tc.wasThere)
		}
	}
}

// TestAudioParamSettingsPathIsOneSpelling: a refusal, a notice and CONFIG.md
// must name the same knob the same way. The path is built (never typed at a
// call site), so this test exists to keep the builder from being bypassed.
func TestAudioParamSettingsPathIsOneSpelling(t *testing.T) {
	got := AudioParamSettingsPath(AudioEngineStreaming, AudioParamKeyEndpoint)
	want := AudioEngineParamSettingsKey + "." + AudioEngineStreaming + "." + AudioParamKeyEndpoint
	if got != want {
		t.Errorf("AudioParamSettingsPath = %q, want %q", got, want)
	}
	// Every knob the block tables list must have a path that the credential
	// predicate can be asked about — a knob reachable only by a hand-written
	// string would be a knob the renderer cannot redact.
	for block, fields := range audioBlockFields {
		if len(fields) == 0 {
			t.Errorf("block %q accepts no knobs — an empty table is a silent no-op", block)
		}
		for _, f := range fields {
			if p := AudioParamSettingsPath(block, f); !strings.HasPrefix(p, AudioEngineParamSettingsKey+"."+block+".") {
				t.Errorf("path %q for %s.%s is not under the nested key", p, block, f)
			}
		}
	}
}

// TestResolveAudioSaucReadsTheCredentialEnvironment is the ONE-reader contract
// for the v3 block's two new knobs (2026-10-14): the pair arrives through the
// SAME env names the streaming block publishes, the settings block answers when
// the environment is silent, and the PRE-RENAME names still work at the same
// rung — the migration policy `overEnv` implements for every other knob.
func TestResolveAudioSaucReadsTheCredentialEnvironment(t *testing.T) {
	path := writeSettings(t, `{"audio_engine_param":{"sauc":{"app_id":"block-app","token":"block-token",`+
		`"endpoint":"wss://h/sauc","resource_id":"volc.x"}}}`)
	cfg, err := FromSettingsFile(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	// Nothing in the environment: the block answers, and the transport knobs
	// (which have no env channel by design) come through untouched.
	got := ResolveAudioSauc(cfg)
	if got.AppID != "block-app" || got.Token != "block-token" {
		t.Errorf("the block's pair did not resolve: %+v", got)
	}
	if got.Endpoint != "wss://h/sauc" || got.ResourceID != "volc.x" {
		t.Errorf("the transport knobs did not resolve: %+v", got)
	}

	// The environment outranks the block, and the CURRENT name beats the
	// pre-rename one when both are set.
	t.Setenv(DoubaoAppIDEnv, "legacy-app")
	got = ResolveAudioSauc(cfg)
	if got.AppID != "legacy-app" {
		t.Errorf("app_id = %q, want the pre-rename env name to outrank the block", got.AppID)
	}
	t.Setenv(AudioAppIDEnv, "env-app")
	t.Setenv(AudioTokenEnv, "env-token")
	got = ResolveAudioSauc(cfg)
	if got.AppID != "env-app" || got.Token != "env-token" {
		t.Errorf("the environment did not outrank the block: %+v", got)
	}
	// A nil Config is the "no settings document" case: the environment alone
	// must still produce a pair.
	if got := ResolveAudioSauc(nil); got.AppID != "env-app" || got.Token != "env-token" {
		t.Errorf("ResolveAudioSauc(nil) = %+v, want the environment pair", got)
	}
}

// TestSaucBlockAcceptsTheCredentialKnobs is the accepted-knob TABLE, which is
// what an operator reads out of a refusal: the v3 block must accept exactly the
// four knobs it models — the pair and the two transport values — and must still
// refuse `cluster` and `api_key`, because neither exists on this protocol and a
// block that accepted them would let a configuration that cannot work load
// silently.
func TestSaucBlockAcceptsTheCredentialKnobs(t *testing.T) {
	for _, knob := range []string{AudioParamKeyAppID, AudioParamKeyToken,
		AudioParamKeyEndpoint, AudioParamKeyResourceID} {
		if !containsString(audioBlockFields[AudioEngineSauc], knob) {
			t.Errorf("the sauc block does not accept %q", knob)
		}
	}
	for _, knob := range []string{AudioParamKeyCluster, AudioParamKeyAPIKey} {
		if containsString(audioBlockFields[AudioEngineSauc], knob) {
			t.Errorf("the sauc block accepts %q, which this protocol has no field for", knob)
		}
	}
	// And the refusal for a knob it does NOT accept names what it does — the
	// sentence an operator acts on.
	raw := map[string]json.RawMessage{
		AudioEngineParamSettingsKey: json.RawMessage(
			`{"` + AudioEngineSauc + `":{"` + AudioParamKeyCluster + `":"c1"}}`),
	}
	_, unknown, err := DecodeAudioEngineParam(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(unknown) != 1 || unknown[0].Path != AudioEngineSauc+"."+AudioParamKeyCluster {
		t.Fatalf("unknown set = %+v, want the cluster knob named", unknown)
	}
	if !containsString(unknown[0].Known, AudioParamKeyAppID) {
		t.Errorf("the refusal does not name the accepted knobs: %v", unknown[0].Known)
	}
}
