package config

// audio_engine_param.go — THE PER-ENGINE PARAMETER BLOCKS, and the ONE place
// the pre-rename top-level keys are folded into them.
//
// THE SHAPE. settings.json carries one nested object per engine:
//
//	{
//	  "audio_engine": "recording",
//	  "audio_engine_param": {
//	    "recording": { "api_key": "...", "key_file": "...", "api_base_url": "...",
//	                   "resource_id": "...", "hotwords": "..." },
//	    "streaming": { "app_id": "...", "token": "...", "key_file": "...",
//	                   "endpoint": "wss://...", "cluster": "..." },
//	    "local":     { "models_dir": "/path/to/weights", "runtime_bin": "/path/to/bin" }
//	  }
//	}
//
// WHY THE THREE BLOCKS ARE SEPARATE TYPES RATHER THAN ONE STRUCT WITH A UNION OF
// FIELDS. The two cloud engines' parameter surfaces were MEASURED against each
// other and do not unify (the measurement is stated in full in
// devdoc/audio-engine-config-surface.md §2): the batch engine authenticates with
// a single API key sent as X-Api-Resource-Id-selected requests over TWO HTTP URLs
// with a submit-then-poll protocol, while the streaming engine authenticates with
// an APPID+TOKEN PAIR carried BOTH in an Authorization header and inside every
// binary frame, over ONE wss:// socket. A single struct would therefore have to
// hold `api_key` OR `app_id`+`token`, `api_base_url` OR `endpoint`, and nothing
// in the type would say that setting half of each pair leaves an engine that
// cannot run. Three types say it: the recording block has no `app_id` field to
// set, so a knob that belongs to the other engine cannot be set by accident and
// cannot be silently ignored.
//
// RULE 15a (omitempty). Every field here is an OPTIONAL string whose zero value
// IS the meaning "not configured" and whose resolution falls through to the next
// rung — so `omitempty` is correct on all of them, and a key absent from the
// document is indistinguishable from one set to "", which is exactly the
// semantics every other knob in this package has. The AUDIO ENGINE SELECTOR
// (Config.AudioEngine) is the same case: "" means "nobody chose" and the
// resolver's terminal default answers it.

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// AudioRecordingParams is the `recording` block: the cloud BATCH engine's
// parameters. Empty on any field means "not configured".
type AudioRecordingParams struct {
	// APIKey is the vendor's single API key, inline. NEVER printed, never
	// logged: `gopi config show` redacts it by LEAF name (AudioCredentialParamNames).
	APIKey string `json:"api_key,omitempty"`
	// KeyFile is the file the key may live in instead (one file knob shared
	// with the streaming block: a credential is located once per host).
	KeyFile string `json:"key_file,omitempty"`
	// APIBaseURL is the endpoint ROOT both of this engine's URLs derive from.
	APIBaseURL string `json:"api_base_url,omitempty"`
	// ResourceID pins the X-Api-Resource-Id header.
	ResourceID string `json:"resource_id,omitempty"`
	// Hotwords is the comma-separated hotword list.
	Hotwords string `json:"hotwords,omitempty"`
}

// AudioStreamingParams is the `streaming` block: the cloud REAL-TIME engine's
// parameters. Its credential is a PAIR (AppID + Token) and its transport is one
// WebSocket — neither shape exists in the recording block, which is the whole
// reason the blocks are separate types.
type AudioStreamingParams struct {
	AppID string `json:"app_id,omitempty"`
	Token string `json:"token,omitempty"`
	// KeyFile is the KEY=VALUE credential document this engine has always read
	// (an APPID line and a TOKEN line). One knob, one location — the same file
	// the recording engine may read for its single-line key.
	KeyFile string `json:"key_file,omitempty"`
	// Endpoint is the wss:// URL. A value that is not wss:// is refused by the
	// pre-flight check, naming the knob, before any audio is opened.
	Endpoint string `json:"endpoint,omitempty"`
	// Cluster selects the product line the request is billed against; the
	// vendor's authorisation is decided by it, so a wrong value is a
	// not-granted refusal rather than a fallback.
	Cluster string `json:"cluster,omitempty"`
}

// AudioSaucParams is the `sauc` block: the v3 SAUC streaming engine's
// parameters. Same SHAPE of work as `streaming` (one WebSocket, audio uploaded
// while it is transcribed) and a different protocol, a different product line
// and a different credential shape — which is why it is its own block rather
// than a key inside the streaming one.
//
// IT HAS NO `cluster` AND NO `api_key`, and the absences are the point. The v3
// endpoint selects the product line by X-Api-Resource-Id alone (there is no
// cluster in the v3 request at all), and its credential is the legacy console
// PAIR — the single-key shape answers 403 on this vendor's endpoint (measured
// 2026-10-13). A block that could hold either value would let an operator write
// a configuration that cannot work.
//
// IT CARRIES THE CREDENTIAL AS OF 2026-10-14, and that is a deliberate reversal
// of the paragraph this block used to end with. Until then the pair could only
// live in a VENDOR-OWNED file (`~/.doubao/v2_voice.env`), so a gopi deployment
// that was correct everywhere else silently depended on a directory belonging to
// another program — the asymmetry the `streaming` block had already closed for
// the same vendor pair (see DoubaoAppIDSettingsKey). The keys are `app_id` and
// `token`, the vocabulary that block already uses for the same two values
// (Rule 13: one spelling of a key), and they are the SAME credential read by the
// SAME file knob below it — the inline pair does not replace the file, it is the
// rung above it.
type AudioSaucParams struct {
	// AppID / Token are the v3 engine's credential PAIR, inline in this block —
	// the vendor's application id and its access token, the shape `streaming`
	// stores beside them. NEVER printed: `gopi config show` redacts a
	// credential by LEAF name (AudioCredentialParamNames), which is why the
	// leaf spellings are the streaming block's and not new ones.
	//
	// A HALF-STATED PAIR IS REFUSED BY NAME rather than completed from the file
	// (the two halves are ONE credential; mixing a stated half with a file's
	// other half authenticates as neither application). The refusal lives in the
	// ONE resolver the CLI owns — cmd/agent/cli_voice_sauc.go — because it has
	// to know which channel supplied the other half.
	AppID string `json:"app_id,omitempty"`
	Token string `json:"token,omitempty"`
	// Endpoint is the wss:// URL of the v3 endpoint. Empty means the package's
	// published default (doubaosauc.DefaultEndpoint —
	// `wss://openspeech.bytedance.com/api/v3/sauc/bigmodel_nostream`), which is
	// the FILE-semantics shape voice_input's contract wants: one audio file, one
	// transcript.
	Endpoint string `json:"endpoint,omitempty"`
	// ResourceID is the X-Api-Resource-Id product line, default
	// `volc.seedasr.sauc.duration` (豆包流式语音识别模型 2.0). A resource the
	// account was never granted is a 403 from the SERVICE, never a fallback.
	ResourceID string `json:"resource_id,omitempty"`
}

// AudioLocalParams is the `local` block: the ON-MACHINE engine's deployment.
// It is this engine's FIRST configuration surface — until 2026-10-09 the only
// channel was the --models / --runtimeBINARY flags, so a headless or repeated
// invocation had to spell two absolute paths every time and nothing could pin
// them per host.
type AudioLocalParams struct {
	// ModelsDir is the directory holding the weights (encoder + llm). A path
	// set here is EXPLICIT: the two-stage deployment search may not override it
	// (voiceResolveDeployment, modelsExplicit).
	ModelsDir string `json:"models_dir,omitempty"`
	// RuntimeBin is the runtime executable (llama-funasr-cli). Same explicitness
	// contract as ModelsDir.
	RuntimeBin string `json:"runtime_bin,omitempty"`
}

// AudioEngineParam is the whole nested object, one field per engine.
type AudioEngineParam struct {
	Recording AudioRecordingParams `json:"recording,omitempty"`
	Streaming AudioStreamingParams `json:"streaming,omitempty"`
	Sauc      AudioSaucParams      `json:"sauc,omitempty"`
	Local     AudioLocalParams     `json:"local,omitempty"`
}

// AudioEngineUnknown is one block or knob written in `audio_engine_param` that
// this build does not model, with the path it was found at.
type AudioEngineUnknown struct {
	// Path is "streaming.foo" (a knob inside a known block) or "batch" (a block
	// that does not exist at all) — one spelling, so a message and a test name
	// the same thing (Rule 13).
	Path string
	// Known lists what IS accepted at that level, so the message can say what
	// to write instead of only what is wrong.
	Known []string
}

// audioBlockNames are the accepted block names in report order.
var audioBlockNames = []string{AudioEngineRecording, AudioEngineStreaming, AudioEngineSauc, AudioEngineLocal}

// audioBlockFields maps each block to the knob names it accepts INSIDE it. ONE
// table (Rule 13): the unknown-knob report, the help text and the tests all read
// it rather than each re-listing the fields.
var audioBlockFields = map[string][]string{
	AudioEngineRecording: {
		AudioParamKeyAPIKey, AudioParamKeyKeyFile, AudioParamKeyAPIBaseURL,
		AudioParamKeyResourceID, AudioParamKeyHotwords,
	},
	AudioEngineStreaming: {
		AudioParamKeyAppID, AudioParamKeyToken, AudioParamKeyKeyFile,
		AudioParamKeyEndpoint, AudioParamKeyCluster,
	},
	AudioEngineSauc: {
		AudioParamKeyAppID, AudioParamKeyToken, AudioParamKeyEndpoint, AudioParamKeyResourceID,
	},
	AudioEngineLocal: {AudioParamKeyModelsDir, AudioParamKeyRuntimeBin},
}

// AudioLegacyKeyReplacement is the migration table: which pre-rename TOP-LEVEL
// settings key became which knob INSIDE which block. It exists so the
// deprecation notice (`gopi tool voice_input`, `gopi chat /voice`) and
// `gopi config show`'s migration row say the SAME destination — a hand-written
// sentence per key is how one of them ends up pointing at a knob that does not
// exist.
type AudioLegacyKeyReplacement struct {
	// Old is the retired top-level settings key.
	Old string
	// Block and Param name the knob that replaced it ("streaming" / "endpoint").
	Block string
	Param string
}

// AudioLegacyKeyReplacements is that table, in the order gopi's messages list
// it. DoubaoKeyFileSettingsKey appears once: the SAME old key fed both cloud
// engines (one knob, two formats) and its replacement is named once per block.
var AudioLegacyKeyReplacements = []AudioLegacyKeyReplacement{
	{DoubaoAPIKeySettingsKey, AudioEngineRecording, AudioParamKeyAPIKey},
	{DoubaoKeyFileSettingsKey, AudioEngineRecording, AudioParamKeyKeyFile},
	{DoubaoBaseURLSettingsKey, AudioEngineRecording, AudioParamKeyAPIBaseURL},
	{DoubaoResourceIDSettingsKey, AudioEngineRecording, AudioParamKeyResourceID},
	{DoubaoHotwordsSettingsKey, AudioEngineRecording, AudioParamKeyHotwords},
	{DoubaoKeyFileSettingsKey, AudioEngineStreaming, AudioParamKeyKeyFile},
	{DoubaoStreamEndpointSettingsKey, AudioEngineStreaming, AudioParamKeyEndpoint},
	{DoubaoAppIDSettingsKey, AudioEngineStreaming, AudioParamKeyAppID},
	{DoubaoTokenSettingsKey, AudioEngineStreaming, AudioParamKeyToken},
}

// AudioParamSettingsPath renders a block knob's full path the way a message
// spells it: `audio_engine_param.streaming.endpoint`. One spelling (Rule 13) so
// a refusal, a notice and CONFIG.md cannot name the same knob three ways.
func AudioParamSettingsPath(block, param string) string {
	return AudioEngineParamSettingsKey + "." + block + "." + param
}

// normalizeAudioParams folds the pre-rename top-level keys into the nested
// blocks, AFTER the env channels have had their say.
//
// THE PRECEDENCE IT IMPLEMENTS, once (Rule 2):
//
//	the nested block (new)  >  the top-level legacy key  >  built-in default
//
// The new spelling wins because the operator who wrote it is the one who read
// the migration notice; the old key still answers because a file nobody has
// edited yet must keep working. There is no third rule and no per-knob
// exception: every fold below is the same `if new == "" { new = legacy }`.
//
// It runs AFTER ApplyEnv so that an env var can never be overridden by a legacy
// settings key it outranks — the fold happens once, at the bottom of the load
// path, and everything above it (flag, env, then this) reads in the order the
// rest of the package uses.
//
// A fold HAPPENS ONLY WHEN THE FIELD IS EMPTY. That is what makes the new
// spelling authoritative even when both are present: an operator mid-migration
// who sets one knob on the new surface and leaves the rest on the old one gets
// exactly that, knob by knob, rather than one surface silently winning wholesale.
func (c *Config) normalizeAudioParams() {
	if c == nil {
		return
	}
	fold := func(dst *string, legacy string) {
		if strings.TrimSpace(*dst) == "" {
			*dst = strings.TrimSpace(legacy)
		}
	}
	// The engine selector: the old NAME "doubao" is translated to the functional
	// name it means, and the translation is reported by AudioLegacyChannelNote
	// (this function is silent — a normaliser that printed would print once per
	// caller, and the resolver runs several times per invocation).
	if strings.TrimSpace(c.AudioEngine) == "" {
		c.AudioEngine = strings.TrimSpace(c.LegacyVoiceEngine)
	}
	if translated, wasLegacy := AudioEngineLegacyVendorName(strings.TrimSpace(c.AudioEngine)); wasLegacy {
		c.AudioEngine = translated
	}

	fold(&c.AudioEngineParam.Recording.APIKey, c.LegacyDoubaoAPIKey)
	fold(&c.AudioEngineParam.Recording.KeyFile, c.LegacyDoubaoKeyFile)
	fold(&c.AudioEngineParam.Recording.APIBaseURL, c.LegacyDoubaoBaseURL)
	fold(&c.AudioEngineParam.Recording.ResourceID, c.LegacyDoubaoResourceID)
	fold(&c.AudioEngineParam.Recording.Hotwords, c.LegacyDoubaoHotwords)

	// The streaming block has no legacy equivalent for its APPID/TOKEN other
	// than the same old keys — one old key, one destination per block, which is
	// why the fold is written out rather than looped over the table: the KEY
	// FILE is the one entry that feeds two blocks, and a loop would have to
	// special-case it anyway.
	fold(&c.AudioEngineParam.Streaming.AppID, c.LegacyDoubaoAppID)
	fold(&c.AudioEngineParam.Streaming.Token, c.LegacyDoubaoToken)
	fold(&c.AudioEngineParam.Streaming.Endpoint, c.LegacyDoubaoStreamEndpoint)
	fold(&c.AudioEngineParam.Streaming.KeyFile, c.LegacyDoubaoKeyFile)
}

// AudioLegacyKeyNote renders the sentence for ONE pre-rename settings key that
// the document still carries: the key, and the path to write instead. Empty
// when the document carries none.
//
// It is a NOTE, not a refusal (the value is still honoured — see
// normalizeAudioParams): the migration is the operator's to make, and gopi's job
// is to make sure it is not made by accident, silently, six months later.
func (c *Config) AudioLegacyKeyNote() string {
	if c == nil {
		return ""
	}
	var found []string
	seen := map[string]bool{}
	for _, r := range AudioLegacyKeyReplacements {
		if seen[r.Old] {
			continue
		}
		if !c.settingsCarriesRawKey(r.Old) {
			continue
		}
		seen[r.Old] = true
		found = append(found, fmt.Sprintf("%q → %s", r.Old, AudioParamSettingsPath(r.Block, r.Param)))
	}
	if len(found) == 0 {
		return ""
	}
	return fmt.Sprintf("settings %s still carries the pre-rename key(s) %s — gopi still honours them; "+
		"write the nested path instead and the value moves with its engine",
		c.SettingsPath, strings.Join(found, ", "))
}

// AudioLegacyEnvNote is the environment half of the same sentence: which
// pre-rename env names are set in THIS process, and where their values now
// belong. Empty when none is set.
func AudioLegacyEnvNote(lookup func(string) string) string {
	if lookup == nil {
		return ""
	}
	var found []string
	for _, r := range AudioLegacyKeyReplacements {
		env, ok := audioLegacyEnvFor(r.Old)
		if !ok || strings.TrimSpace(lookup(env)) == "" {
			continue
		}
		path := AudioParamSettingsPath(r.Block, r.Param)
		entry := fmt.Sprintf("$%s → %s", env, path)
		if !containsString(found, entry) {
			found = append(found, entry)
		}
	}
	if len(found) == 0 {
		return ""
	}
	return fmt.Sprintf("the environment still names the pre-rename variable(s) %s — gopi still honours them; "+
		"the current variable for each parameter is in `gopi --help-env`",
		strings.Join(found, ", "))
}

// audioLegacyEnvFor maps a retired settings key onto the env name that used to
// carry the same value.
func audioLegacyEnvFor(oldKey string) (string, bool) {
	switch oldKey {
	case DoubaoAPIKeySettingsKey:
		return DoubaoAPIKeyEnv, true
	case DoubaoKeyFileSettingsKey:
		return DoubaoKeyFileEnv, true
	case DoubaoBaseURLSettingsKey:
		return DoubaoBaseURLEnv, true
	case DoubaoResourceIDSettingsKey:
		return DoubaoResourceIDEnv, true
	case DoubaoHotwordsSettingsKey:
		return DoubaoHotwordsEnv, true
	case DoubaoAppIDSettingsKey:
		return DoubaoAppIDEnv, true
	case DoubaoTokenSettingsKey:
		return DoubaoTokenEnv, true
	case DoubaoStreamEndpointSettingsKey:
		return DoubaoStreamEndpointEnv, true
	}
	return "", false
}

// containsString lives in settings_write_guard.go — one helper for the whole
// package (Rule 14), reused rather than re-declared here.

// ── the ONE reader of each parameter's environment channel (Rule 2) ────────
//
// THE PRECEDENCE, the same for every knob on this surface:
//
//	CLI flag  >  $GOPI_AUDIO_*  >  audio_engine_param.<block>.<knob>  >  built-in default
//
// The flag rung belongs to cmd/agent (it parses the arguments); the two rungs
// below it are these functions, so no call site re-implements the order and two
// of them cannot disagree about which engine a run used.
//
// The PRE-RENAME environment names are read here too, at the SAME rung as their
// successors (a shell script that exports GOPI_DOUBAO_APP_ID is not less
// explicit than one that exports GOPI_AUDIO_APP_ID), with the newer name
// winning when both are set. The notice that says so is rendered once per run
// by AudioLegacyEnvNote.

// envThen returns the first non-empty of the new env name, then the legacy one.
func envThen(newName, legacyName string) string {
	if v := strings.TrimSpace(os.Getenv(newName)); v != "" {
		return v
	}
	return strings.TrimSpace(os.Getenv(legacyName))
}

// overEnv applies the ENVIRONMENT rung to a field: the current name first, the
// PRE-RENAME name at the SAME RUNG when the current one is unset.
//
// This is the whole migration policy in one function, and the reason it is a
// function (Rule 2): a legacy name applied BELOW the settings rung would stop
// outranking the file the moment the file carried any value, so an operator who
// exports the variable they have always exported would be silently ignored —
// the failure mode a rename must not have.
func overEnv(dst *string, env, legacyEnv string) {
	if v := strings.TrimSpace(os.Getenv(env)); v != "" {
		*dst = v
		return
	}
	if v := strings.TrimSpace(os.Getenv(legacyEnv)); v != "" {
		*dst = v
	}
}

// ResolveAudioRecording returns the BATCH engine's parameters with the
// environment applied over the settings block.
func ResolveAudioRecording(cfg *Config) AudioRecordingParams {
	var p AudioRecordingParams
	if cfg != nil {
		p = cfg.AudioEngineParam.Recording
	}
	// The credential's env channel is READ but never republished (see
	// DoubaoAPIKeyEnv): an operator who exports it has made that trade knowingly.
	overEnv(&p.APIKey, AudioAPIKeyEnv, DoubaoAPIKeyEnv)
	overEnv(&p.KeyFile, AudioKeyFileEnv, DoubaoKeyFileEnv)
	overEnv(&p.APIBaseURL, AudioAPIBaseURLEnv, DoubaoBaseURLEnv)
	overEnv(&p.ResourceID, AudioResourceIDEnv, DoubaoResourceIDEnv)
	overEnv(&p.Hotwords, AudioHotwordsEnv, DoubaoHotwordsEnv)
	return p
}

// ResolveAudioStreaming returns the REAL-TIME engine's parameters with the
// environment applied over the settings block.
func ResolveAudioStreaming(cfg *Config) AudioStreamingParams {
	var p AudioStreamingParams
	if cfg != nil {
		p = cfg.AudioEngineParam.Streaming
	}
	overEnv(&p.AppID, AudioAppIDEnv, DoubaoAppIDEnv)
	overEnv(&p.Token, AudioTokenEnv, DoubaoTokenEnv)
	overEnv(&p.Endpoint, AudioStreamEndpointEnv, DoubaoStreamEndpointEnv)
	// The key FILE is ONE knob feeding two blocks: the recording block's
	// resolved value is the answer, so a caller cannot end up with two
	// different files named by the same setting.
	p.KeyFile = ResolveAudioRecording(cfg).KeyFile
	return p
}

// ResolveAudioSauc returns the v3 SAUC engine's parameters with the environment
// applied over the settings block.
//
// THE CREDENTIAL IS HERE AS OF 2026-10-14, and the env names are the SAME two
// the streaming block uses — $GOPI_AUDIO_APP_ID / $GOPI_AUDIO_TOKEN, with the
// pre-rename $GOPI_DOUBAO_APP_ID / $GOPI_DOUBAO_TOKEN at the SAME rung (the
// migration policy stated once, overEnv below). One vendor issues one
// application id and one access token per account; a second pair of env names
// for the same two values would be a second name an operator has to discover
// before the value they already export can be used (Rule 13/14).
//
// The two TRANSPORT knobs this block holds are still settings-only, and that
// stays a decision rather than an omission: a URL and a product-line id are
// neither of them secret, and the surface already has seven environment
// channels. Adding one is a rule-change, not a gap (Rule 35: no new surface
// without an observed failure it fixes).
//
// WHERE THE CREDENTIAL ITSELF COMES FROM IS NOT DECIDED HERE. This function
// answers "what do the settings document and the environment say" — the rungs
// above the FILE. Which of {an explicitly named file, this pair, the legacy
// doubao file} a run ends on is one resolver's business
// (cmd/agent/cli_voice_sauc.go, voiceSaucCredentialSource), because that
// decision needs the CLI's flag rung and the one key-FILE knob, neither of
// which exists at this layer.
func ResolveAudioSauc(cfg *Config) AudioSaucParams {
	var p AudioSaucParams
	if cfg != nil {
		p = cfg.AudioEngineParam.Sauc
	}
	overEnv(&p.AppID, AudioAppIDEnv, DoubaoAppIDEnv)
	overEnv(&p.Token, AudioTokenEnv, DoubaoTokenEnv)
	return p
}

// ResolveAudioLocal returns the ON-MACHINE engine's deployment with the
// environment applied over the settings block — this engine's FIRST settings and
// environment surface (before 2026-10-09 the only channel was the CLI flag).
func ResolveAudioLocal(cfg *Config) AudioLocalParams {
	var p AudioLocalParams
	if cfg != nil {
		p = cfg.AudioEngineParam.Local
	}
	if v := strings.TrimSpace(os.Getenv(AudioLocalModelsEnv)); v != "" {
		p.ModelsDir = v
	}
	if v := strings.TrimSpace(os.Getenv(AudioLocalRuntimeEnv)); v != "" {
		p.RuntimeBin = v
	}
	return p
}

// AudioEngineUnknownNote is AudioEngineUnknownError's non-fatal spelling: the
// sentence to PRINT when the settings document carries a block or knob this
// build does not model, or "" when it carries none.
//
// It is a note rather than a hard load failure on purpose, and the line is worth
// stating: a document with an extra key must keep LOADING (the same tolerance
// SettingsCarriesKey's comment documents for a retired key — gopi must not
// refuse to start because a newer build wrote a knob an older one lacks), but it
// must not be silent either (Rule 15). The run says what it ignored, by name.
func (c *Config) AudioEngineUnknownNote() string {
	if c == nil || len(c.audioEngineParamUnknown) == 0 {
		return ""
	}
	err := AudioEngineUnknownError(c.audioEngineParamUnknown)
	if err == nil {
		return ""
	}
	return err.Error()
}

// settingsCarriesRawKey reports whether the settings DOCUMENT this Config was
// read from literally carries key at the top level. It is the file question, not
// the struct question, for the reason SettingsCarriesKey documents — but scoped
// to the decode this package already did, so a caller that has a Config does not
// pay a second read.
func (c *Config) settingsCarriesRawKey(key string) bool {
	if c == nil || c.rawTopLevel == nil {
		return false
	}
	_, ok := c.rawTopLevel[key]
	return ok
}

// DecodeAudioEngineParam reads the `audio_engine_param` object out of a raw
// settings document into the typed blocks, and reports every block or knob the
// document carries that this build does not model.
//
// WHY IT IS A SEPARATE DECODE rather than a struct tag. encoding/json ignores
// unknown fields silently, which is the one thing this surface may not do: an
// operator who typos `recording.api_ky` would otherwise get a run that resolves
// to the built-in default and no message at all (Rule 15). Decoding the object a
// second time as map[string]json.RawMessage costs one parse of a small object and
// buys the ability to NAME what was ignored.
//
// A malformed `audio_engine_param` (not an object) is an ERROR, not a silent
// skip: the document says something about the engine parameters and gopi cannot
// read it, which must not resolve to "nothing was configured".
func DecodeAudioEngineParam(raw map[string]json.RawMessage) (AudioEngineParam, []AudioEngineUnknown, error) {
	var out AudioEngineParam
	if raw == nil {
		return out, nil, nil
	}
	blob, ok := raw[AudioEngineParamSettingsKey]
	if !ok {
		return out, nil, nil
	}
	var blocks map[string]json.RawMessage
	if err := json.Unmarshal(blob, &blocks); err != nil {
		return out, nil, fmt.Errorf("settings %q must be an object keyed by engine name (%s): %w",
			AudioEngineParamSettingsKey, strings.Join(audioBlockNames, ", "), err)
	}
	if err := json.Unmarshal(blob, &out); err != nil {
		return out, nil, fmt.Errorf("settings %q: %w", AudioEngineParamSettingsKey, err)
	}
	var unknown []AudioEngineUnknown
	names := make([]string, 0, len(blocks))
	for name := range blocks {
		names = append(names, name)
	}
	sort.Strings(names) // stable report order, so a message and a test agree
	for _, name := range names {
		fields, known := audioBlockFields[name]
		if !known {
			unknown = append(unknown, AudioEngineUnknown{Path: name, Known: audioBlockNames})
			continue
		}
		var knobs map[string]json.RawMessage
		if err := json.Unmarshal(blocks[name], &knobs); err != nil {
			return out, nil, fmt.Errorf("settings %s must be an object: %w",
				AudioParamSettingsPath(name, ""), err)
		}
		knobNames := make([]string, 0, len(knobs))
		for k := range knobs {
			knobNames = append(knobNames, k)
		}
		sort.Strings(knobNames)
		for _, k := range knobNames {
			if containsString(fields, k) {
				continue
			}
			unknown = append(unknown, AudioEngineUnknown{
				Path:  name + "." + k,
				Known: fields,
			})
		}
	}
	return out, unknown, nil
}

// AudioEngineUnknownError renders the unknown-block/unknown-knob list as the
// error a caller reports, or nil when there is nothing to report. One spelling
// (Rule 13): the CLI's refusal, its tests and CONFIG.md quote the same shape.
func AudioEngineUnknownError(unknown []AudioEngineUnknown) error {
	if len(unknown) == 0 {
		return nil
	}
	parts := make([]string, 0, len(unknown))
	for _, u := range unknown {
		parts = append(parts, fmt.Sprintf("%q (accepted here: %s)", u.Path, strings.Join(u.Known, ", ")))
	}
	return fmt.Errorf("settings %q carries %s — gopi does not ignore it: fix or remove "+
		"the name(s) and run again", AudioEngineParamSettingsKey, strings.Join(parts, "; "))
}
