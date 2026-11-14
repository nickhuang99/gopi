package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/nick/gopi/internal/boolvocab"
	"github.com/nick/gopi/internal/scratch"
)

// SkipClaimBroadcastEnv is the claim-broadcast kill-switch: a caller that
// manages the remote CAS itself sets it to "1" and the local claim stands alone
// (cmd/agent's claim-lock branch).
//
// It is EXPORTED here (Rule 14) because it has TWO readers that must name the
// same variable: the production claim path and the hermetic worker tests, which
// arm it so a single-node fixture never spawns an ssh leg. Before this constant
// both sides spelled the literal (Rule 1b/13) — the test fixture's copy and the
// code's copy were one typo apart from disagreeing.
const SkipClaimBroadcastEnv = "GOPI_SKIP_CLAIM_BROADCAST"

// The three operator knobs below were advertised by their own help pages
// (`gopi task -h`'s sweep-worktrees paragraph, `gopi worker`'s Environment
// block) while no catalog row carried them, so `gopi --help-env` — the surface
// the help itself points an operator at — named none of them, and CONFIG.md
// neither. Their READERS stay where they belong (Rule 2: one reader each —
// internal/tools/worktreeSweepOptionsFrom, cmd/agent's workerIdleInterval), and
// those readers now alias these constants instead of re-typing the literal, the
// same direction internal/tools already uses for MemorySyncTimeoutEnv: the
// literal is declared ONCE, here, and every consumer references it (Rules
// 1b/13/14). A row plus a referencing reader is the pair the catalog is for.
const (
	// WorktreeSweepMinAgeEnvName is the age threshold (hours) below which
	// `gopi task sweep-worktrees` never reclaims a registration.
	WorktreeSweepMinAgeEnvName = "GOPI_WORKTREE_SWEEP_MIN_AGE_H"
	// WorktreeSweepMaxEnvName caps how many registrations one sweep run
	// reclaims — the blast-radius throttle.
	WorktreeSweepMaxEnvName = "GOPI_WORKTREE_SWEEP_MAX"
	// WorkerIdleIntervalEnv is the drained-queue re-check cadence (seconds) of
	// `gopi worker` (the long-running tick; `--once` exits instead of waiting).
	WorkerIdleIntervalEnv = "GOPI_WORKER_IDLE_SEC"
	// BrewIntervalEnv paces the session-exit brew trigger (seconds; 0 disables
	// it). Found by the help-advertised-env gate in the SAME sweep as the three
	// above: `gopi memory brew -h` advertised it while no catalog row carried it.
	BrewIntervalEnv = "GOPI_BREW_INTERVAL_SEC"
	// TaskDeliveryProofAllowEnv is the operator's escape from the close-out
	// delivery proof: ON releases the `completed` writeback whose claim could
	// not be measured, and the release is RECORDED on the row's trail
	// (`delivery-proof-unverified`) rather than silently applied. Same family
	// and same shape as the dirty-close-out allow above, and catalogued for the
	// same reason the three sweep knobs were: `gopi task update -h` advertises
	// it, so `gopi --help-env` — the surface that page points the reader at —
	// must list it. The escape's typed twin is `gopi task update <row> --force`;
	// both faces are ONE decision (tools.deliveryProofAllowanceFor), so the two
	// spellings cannot diverge. Declared here because the catalog below and that
	// reader must name the same variable (Rule 14: one definition, aliased
	// never mirrored).
	TaskDeliveryProofAllowEnv = "GOPI_TASK_DELIVERY_PROOF_ALLOW"
)

// SettingsEnv is the env var that overrides the settings.json path
// (GOPI_SETTINGS), read in exactly one place per resolution path (Rule 2).
const SettingsEnv = "GOPI_SETTINGS"

// VoiceDeviceEnv is the env var that supplies the capture client's configured
// default device — tier 2 of the device-selection policy (explicit --device >
// configured default > system default capture > first enumerated; see
// devdoc/voice-input-client.md). Its value is a device id as printed by
// `gopi tool voice_input --client --list-devices`, i.e. a NAME-addressed device,
// never a card index. Defined here (not in cmd/agent) because the catalog below
// and the policy reader must name the same variable (Rule 14).
const VoiceDeviceEnv = "GOPI_VOICE_DEVICE"

// VoiceDeviceSettingsKey is the settings.json key carrying the same knob. It is
// exported so the policy's messages can name the key they actually read
// without re-typing the literal (Rule 13).
const VoiceDeviceSettingsKey = "default_device"

// AudioEngineEnv is the env var that selects `gopi tool voice_input`'s transcription
// engine. It is the middle rung of the three-rung resolution the voice CLI
// implements (--engine flag > this env var > settings.json AudioEngineSettingsKey
// > AudioEngineDefault); see devdoc/doubao-asr-integration.md §3 for the
// argument behind the default, rewritten when it flipped to the cloud engine.
//
// The SPELLING is "audio", not "voice": the knob selects the engine that turns
// AUDIO into text and nothing else in the product is named after it, so the
// surface is named for what it configures rather than for the subcommand that
// happens to read it today (devdoc/audio-engine-config-surface.md §1 states the
// ruling and its cost).
const AudioEngineEnv = "GOPI_AUDIO_ENGINE"

// TokensCountURLEnv names the `count_tokens` endpoint `gopi tool tokens`
// queries for the SERVER's own arithmetic on a request body.
//
// It is a measurement endpoint, not a model: an Anthropic-compatible
// `/v1/messages/count_tokens` on the same host the requests go to. Named here
// (Rule 1/13/14) because the usage page advertises it — a page that advertises
// a variable is a promise, and the catalog is what `gopi --help-env` renders
// from (Rule 21's help_env_catalog_gate).
const TokensCountURLEnv = "GOPI_TOKENS_COUNT_URL"

// TokensCountKeyEnv is the credential the endpoint above is called with.
//
// It rides the SAME narrow channel as the other inline credentials
// (AudioAPIKeyEnv's claim applies verbatim): the process environment is
// inherited by every child gopi spawns, so an inline key is the channel of last
// resort — an operator who can put it in settings.json should.
const TokensCountKeyEnv = "GOPI_TOKENS_COUNT_KEY"

// AudioEngineSettingsKey is the settings.json key carrying the same selection.
// Exported so the CLI's messages name the key they read rather than re-typing
// the literal (Rule 13).
const AudioEngineSettingsKey = "audio_engine"

// AudioEngineParamSettingsKey is the settings.json key holding the PER-ENGINE
// parameter blocks — ONE nested object, keyed by engine name, each block holding
// that engine's own parameters:
//
//	"audio_engine_param": {
//	  "recording": {...}, "streaming": {...}, "local": {...}
//	}
//
// WHY NESTED RATHER THAN FLAT (the design tradeoff the brief demands, reasoned
// in devdoc/audio-engine-config-surface.md §2). A flat surface cannot answer
// "which engine does this knob belong to?" — `audio_api_key` would have to be
// read by the recording arm AND (as APPID) by the streaming arm, and the two
// vendors' credential shapes are provably not the same value under different
// names: a single API key on one side, an APPID+TOKEN PAIR on the other, one
// HTTP root with two derived URLs and polling on one side, one wss:// URL on the
// other. A flat surface either merges knobs that are not the same knob or grows a
// vendor prefix until it is the doubao_* surface again with the vendor's name
// filed off. The nested form makes the engine the OUTER key, so a third engine
// (or a third vendor behind `recording`) adds a BLOCK rather than a namespace of
// top-level keys, and "which knobs may I set for this engine?" is answered by
// reading one object.
const AudioEngineParamSettingsKey = "audio_engine_param"

// AudioEngineRecording is the CLOUD BATCH engine: one transcript per file, the
// whole file uploaded before the first word comes back (录音文件识别).
//
// The three engine VALUES are FUNCTIONAL, not vendor names (Rule 39's spirit at
// one remove): what a caller chooses between is the SHAPE of the work — a whole
// recording uploaded and transcribed, or a live stream transcribed while the
// audio is still being spoken, or an engine running on this machine — and the
// vendor answering that shape today is a fact about the block's parameters, not
// about the caller's decision. A name that carried the vendor would have to
// change (and every operator's settings file with it) the day a second vendor
// answers the same shape.
const (
	AudioEngineRecording = "recording"
	// AudioEngineStreaming is the CLOUD STREAMING engine: one WebSocket, a
	// partial transcript while the audio is still being spoken. Its own value
	// rather than sharing the batch name because a caller must be able to
	// CHOOSE: the shapes differ in the bill (a batch call is priced per audio
	// hour, a stream is billed against the streaming duration package), in
	// latency (seconds before the first word against sub-second partials) and
	// in what the run costs if it is interrupted.
	//
	// It is NEVER the default (AudioEngineDefault stays AudioEngineRecording):
	// streaming is reached by name on one of the three channels, because the
	// streaming resource is a separate grant on the vendor's side and a default
	// that depends on a second grant would break the batch engine's own
	// guarantee that a machine with nothing deployed still transcribes.
	AudioEngineStreaming = "streaming"
	// AudioEngineLocal is the ON-MACHINE engine (Fun-ASR-Nano through
	// llama.cpp): nothing leaves the machine, and it needs a deployment.
	AudioEngineLocal = "local"
	// AudioEngineSauc is the v3 SAUC streaming engine: the SAME SHAPE of work
	// as `streaming` (one WebSocket, audio uploaded while it is transcribed)
	// against a DIFFERENT protocol and a different credential, and it gets its
	// own value for exactly the reason `streaming` got one rather than sharing
	// `recording`: a caller must be able to CHOOSE. The two differ in what they
	// are billed against (v2 `volcengine_streaming_common` against the v3
	// `volc.seedasr.sauc.duration` package), in protocol version (v2's JSON
	// envelope against v3's binary one) and — measured 2026-10-13 — in the
	// CREDENTIAL SHAPE the service accepts at all: the v3 endpoint answers the
	// single-key shape with 403 "requested resource not granted" and the legacy
	// console pair with 101. A single name would force one of those facts to be
	// guessed from a parameter block.
	//
	// It is NEVER the default, for the same reason `streaming` is not: it needs
	// a grant of its own on the vendor's side, and the batch engine's guarantee
	// (a machine with nothing deployed still transcribes) must not depend on a
	// second grant.
	AudioEngineSauc = "sauc"
)

// AudioEngineEngineNames is the accepted set, in the order the help lists them —
// ONE list, referenced by the parser, the validator and the help table (Rule 13).
var AudioEngineEngineNames = []string{AudioEngineLocal, AudioEngineRecording, AudioEngineStreaming, AudioEngineSauc}

// ── the names this surface used BEFORE it was renamed (2026-10-09) ─────────
//
// THEY ARE STILL READ, and each one that answers produces a loud deprecation
// line naming the replacement (Rule 15: a knob that stopped working in silence
// would be worse than one that never existed — an operator's working
// configuration would simply stop taking effect).
//
// They are constants rather than literals typed at each read for the usual
// reason (Rule 1/13): the settings key, the env var and the two engine-name
// aliases are each spelled in more than one place — the reader, the notice, and
// `gopi config show`'s migration row.
const (
	// AudioEngineLegacyEnv / AudioEngineLegacySettingsKey are the engine
	// selector's old channels.
	AudioEngineLegacyEnv         = "GOPI_VOICE_ENGINE"
	AudioEngineLegacySettingsKey = "voice_engine"
	// AudioEngineLegacyVendorRecording / AudioEngineLegacyVendorStreaming are
	// the engine NAMES the old values spelled. They are behaviour, not vanity:
	// a settings file or a script that says "doubao" must keep selecting the
	// batch engine and must say so out loud.
	AudioEngineLegacyVendorRecording = "doubao"
	AudioEngineLegacyVendorStreaming = "doubao-stream"
)

// AudioEngineLegacyVendorName is the ONE lookup that maps a pre-rename engine
// name onto the functional name it means today, or reports that the value needs
// no translation. It exists so the resolver, the settings view and the help
// cannot disagree about what "doubao" names (Rule 2).
func AudioEngineLegacyVendorName(name string) (string, bool) {
	switch name {
	case AudioEngineLegacyVendorRecording:
		return AudioEngineRecording, true
	case AudioEngineLegacyVendorStreaming:
		return AudioEngineStreaming, true
	}
	return name, false
}

// ── the PER-ENGINE PARAMETER surface inside AudioEngineParamSettingsKey ─────
//
// Each block holds ONE engine's parameters, so the engine is the OUTER key and
// a knob never has to carry its engine's name to be unambiguous (the argument
// for the nested shape is on AudioEngineParamSettingsKey). The settings keys
// below are therefore SHORT and block-local, while the ENV NAMES are flat (there
// is one process environment, so a name must say which engine it feeds).
const (
	// AudioParamKeyAPIKey is the BATCH engine's credential INSIDE its block —
	// the vendor's single API key. It has no env var of its own beyond the one
	// below because a credential's channels are deliberately few (see
	// AudioAPIKeyEnv).
	AudioParamKeyAPIKey = "api_key"
	// AudioParamKeyKeyFile is the file holding a credential, common to BOTH
	// cloud blocks: the batch engine reads the single-line API key out of it,
	// the streaming engine reads the KEY=VALUE APPID/TOKEN document out of the
	// same path. ONE file knob with two formats is deliberate — the operator
	// keeps one credential file per host, not one per protocol.
	AudioParamKeyKeyFile = "key_file"
	// AudioParamKeyAPIBaseURL is the BATCH engine's endpoint ROOT, from which
	// BOTH of its URLs derive (doubaoasr.SubmitURLFor / QueryURLFor).
	AudioParamKeyAPIBaseURL = "api_base_url"
	// AudioParamKeyResourceID is the BATCH engine's X-Api-Resource-Id.
	AudioParamKeyResourceID = "resource_id"
	// AudioParamKeyHotwords is the BATCH engine's comma-separated hotword list.
	AudioParamKeyHotwords = "hotwords"
	// AudioParamKeyCluster is the STREAMING engine's product line (the vendor
	// bills and authorises by it; the wrong value is a not-granted refusal).
	AudioParamKeyCluster = "cluster"
	// AudioParamKeyEndpoint is the STREAMING engine's WebSocket URL.
	AudioParamKeyEndpoint = "endpoint"
	// AudioParamKeyModelsDir is the LOCAL engine's weights directory — its
	// FIRST settings/env surface (until 2026-10-09 only --models existed).
	AudioParamKeyModelsDir = "models_dir"
	// AudioParamKeyRuntimeBin is the LOCAL engine's runtime executable — same
	// new surface, same reason.
	AudioParamKeyRuntimeBin = "runtime_bin"
)

// The credential-bearing leaf names, ONE list (Rule 14): `gopi config show`
// redacts by LEAF, because the same credential is a top-level key before the
// rename and a nested one after it, and a renderer that learned the nested
// spelling without the old one would print a live secret for a file that had not
// been migrated yet.
var AudioCredentialParamNames = []string{AudioParamKeyAPIKey, AudioParamKeyAppID, AudioParamKeyToken}

// AudioParamKeyAppID / AudioParamKeyToken are the STREAMING block's credential
// PAIR — the vendor issues an application id beside the token, and neither is
// usable without the other. It is the measured reason the two cloud engines
// cannot share one param block: the batch engine's credential is ONE value
// (`api_key`), the streaming engine's is a PAIR, and a schema that flattened
// them would have to accept a shape neither vendor issues.
const (
	AudioParamKeyAppID = "app_id"
	AudioParamKeyToken = "token"
)

// The ENV channels of the block parameters — flat, and named after the engine
// each feeds. They are the middle rung of every parameter's resolution
// (flag > env > settings block > default).
const (
	AudioAPIKeyEnv         = "GOPI_AUDIO_API_KEY"
	AudioKeyFileEnv        = "GOPI_AUDIO_KEY_FILE"
	AudioAPIBaseURLEnv     = "GOPI_AUDIO_API_BASE_URL"
	AudioResourceIDEnv     = "GOPI_AUDIO_RESOURCE_ID"
	AudioHotwordsEnv       = "GOPI_AUDIO_HOTWORDS"
	AudioAppIDEnv          = "GOPI_AUDIO_APP_ID"
	AudioTokenEnv          = "GOPI_AUDIO_TOKEN"
	AudioStreamEndpointEnv = "GOPI_AUDIO_STREAM_ENDPOINT"
	// AudioLocalModelsEnv / AudioLocalRuntimeEnv are the LOCAL engine's env
	// channels — the first env surface these two knobs have ever had (before
	// 2026-10-09 the only channel was the flag, so a headless deployment had to
	// spell two absolute paths on every invocation).
	AudioLocalModelsEnv  = "GOPI_AUDIO_LOCAL_MODELS"
	AudioLocalRuntimeEnv = "GOPI_AUDIO_LOCAL_RUNTIME"
)

// AudioLegacyParamSettingsKeys names every TOP-LEVEL settings key the rename
// replaced, so ONE list can drive the deprecation reader, the notice and the
// `gopi config show` migration row (Rule 13: three sites that must agree about
// which keys are old).
var AudioLegacyParamSettingsKeys = []string{
	DoubaoAPIKeySettingsKey,
	DoubaoKeyFileSettingsKey,
	DoubaoBaseURLSettingsKey,
	DoubaoResourceIDSettingsKey,
	DoubaoHotwordsSettingsKey,
	DoubaoAppIDSettingsKey,
	DoubaoTokenSettingsKey,
	DoubaoStreamEndpointSettingsKey,
}

// AudioLegacyParamEnvNames is the same list for the ENVIRONMENT half. It exists
// because a shell script that exports the old name must be told — the variable
// is still READ, and the deprecation line names the one to write next.
var AudioLegacyParamEnvNames = []string{
	DoubaoAPIKeyEnv,
	DoubaoKeyFileEnv,
	DoubaoBaseURLEnv,
	DoubaoResourceIDEnv,
	DoubaoHotwordsEnv,
	DoubaoAppIDEnv,
	DoubaoTokenEnv,
	DoubaoStreamEndpointEnv,
}

// AudioEngineDefault is the engine a run uses when NO channel chose one (no
// --engine, no GOPI_AUDIO_ENGINE, no settings "audio_engine") — the TERMINAL
// default of the resolution chain, in ONE place (Rule 13).
//
// It is a named constant rather than a literal spelled at each return because
// the resolver has two terminal paths (the settings file could not be read, and
// the settings file chose nothing) and they MUST agree: two literals is how a
// default drifts in half the paths, which is precisely the shape a flip is
// meant to change. The argument for its VALUE is
// devdoc/doubao-asr-integration.md §3; the argument's short form is that a
// default is what an operator gets on a machine where nothing was deployed, and
// only one of the two engines needs nothing deployed.
//
// It lives in internal/config (next to the two names and the knob that selects
// them) because the CLI's resolver, its messages and its help text all have to
// mean the same engine, and that is the exported-constants package for this
// feature (Rule 14).
const AudioEngineDefault = AudioEngineRecording

// DoubaoKeyFileEnv / DoubaoKeyFileSettingsKey name the API-key file location
// for the doubao engine (default ~/.doubao/apikey.txt).
const (
	DoubaoKeyFileEnv         = "GOPI_DOUBAO_KEY_FILE"
	DoubaoKeyFileSettingsKey = "doubao_key_file"
)

// DoubaoAPIKeySettingsKey is the settings.json key that carries the doubao API
// key VALUE INLINE, and — unlike every other doubao knob — it has NO
// environment channel of its own.
//
// WHY NO ENV VAR, and why that is a decision rather than an omission. gopi
// already publishes the whole settings file down one channel into the process
// environment (`config.ApplyEnv`, internal/config/env.go), and that environment
// is INHERITED BY EVERY CHILD gopi spawns: the bash tool, the agent's own
// children, and the task executor. An env var holding this value would therefore
// be a SECOND, wider copy of the credential, readable from any descendant
// process — while the settings file is 0600. The file is the narrower channel,
// so the inline value has exactly one: the file.
//
// The setting exists because the operator's own configuration for image
// understanding (`vision_model` / `vision_base_url` / `api_key`) already lives
// in this file, and the voice surface should not keep a credential in a
// vendor's own directory (~/.doubao/) while every other knob — and the sibling
// key for the OTHER model behind the same endpoint — lives here.
//
// PRECEDENCE (spelled once, in the resolver cli_voice_engine.go's
// voiceDoubaoKeySource, and documented in CONFIG.md §2c):
//
//	--doubao-key-file  >  $GOPI_DOUBAO_KEY_FILE  >  settings "doubao_key_file"
//	                   >  settings "doubao_api_key" (this key, inline)
//	                   >  ~/.doubao/apikey.txt (the legacy fallback)
//
// A key FILE named on an explicit channel still wins: a stored path is a
// deliberate statement about WHERE the credential lives, and the inline value
// is what fills the gap when nobody made one.
const DoubaoAPIKeySettingsKey = "doubao_api_key"

// DoubaoAPIKeyEnv is the environment channel of the same value. It exists, and
// it is READ (cmd/agent's voiceDoubaoKeySource), because the alternative is a
// catalog row that silently ignores an operator who exports it — the silent
// failure Rule 15 bans. It is deliberately NOT published by config.ApplyEnv:
// every OTHER settings knob is republished into gopi's own environment for its
// children to inherit, and this is the one value that must not become a second,
// wider copy of a credential (the process environment is readable by every
// descendant, the 0600 settings file is not). An operator who exports it has
// made that trade knowingly; gopi never makes it for them, and the catalog's
// help says so.
const DoubaoAPIKeyEnv = "GOPI_DOUBAO_API_KEY"

// DoubaoAPIKeyNotPublishedClaim is the one sentence CONFIG.md §2c and the help
// table both state about this knob's environment channel, so the doc and the
// rendered help cannot drift into two different promises about a credential.
// It is a const because a test asserts the row CARRIES it (Rule 13: the claim
// is referenced, never re-typed).
const DoubaoAPIKeyNotPublishedClaim = "gopi does not republish this value into the environment"

// DoubaoAPIKeyPlaceholder is the illustrative value CONFIG.md and the help
// pages show for DoubaoAPIKeySettingsKey. A DOCUMENTED EXAMPLE MUST NOT BE A
// CREDENTIAL (Rule 39's spirit at one remove): an operator who copies the
// documentation gets a value the service refuses, never a working key, and no
// real key is ever typed into a git-tracked file to show the shape.
const DoubaoAPIKeyPlaceholder = "<doubao-api-key>"

// DoubaoBaseURLEnv / DoubaoBaseURLSettingsKey move the BATCH engine's endpoint
// ROOT, from which BOTH AUC URLs derive (doubaoasr.SubmitURLPath /
// QueryURLPath) — the same shape `vision_base_url` has for the vision
// endpoint, and for the same reason: an operator whose account is served from
// a regional or private deployment sets ONE root rather than two URLs that can
// be made to disagree (a submit on one host polled on another's is a request
// that fails with a task id the poller never saw).
//
// Empty means the published root (doubaoasr.DefaultBaseURL), so a machine with
// no configuration sends exactly the request it sent before this knob existed.
// The per-URL flags --doubao-submit-url / --doubao-query-url remain MORE
// SPECIFIC than this setting and still win for the URL they name; this knob is
// the settings-file face of the same decision.
const (
	DoubaoBaseURLEnv         = "GOPI_DOUBAO_BASE_URL"
	DoubaoBaseURLSettingsKey = "doubao_base_url"
)

// DoubaoStreamEndpointEnv / DoubaoStreamEndpointSettingsKey name the v2
// real-time streaming endpoint — the WebSocket URL of the other doubao
// channel (wss://openspeech.bytedance.com/api/v2/asr by default). It is the
// streaming arm's counterpart of the root above: the same vendor hosts both
// protocols, so a regional deployment needs both moveable, and until now the
// stream URL could be set on NO channel (it was a compile-time constant with
// doubaostream.Options.Endpoint reachable only from Go).
const (
	DoubaoStreamEndpointEnv         = "GOPI_DOUBAO_STREAM_ENDPOINT"
	DoubaoStreamEndpointSettingsKey = "doubao_stream_endpoint"
)

// DoubaoAppIDEnv / DoubaoAppIDSettingsKey and DoubaoTokenEnv /
// DoubaoTokenSettingsKey carry the STREAMING channel's credential (the APPID
// and TOKEN lines of the KEY=VALUE file this engine has always read) into
// settings.json, following the discipline the batch key already follows:
//
//   - the value lives in the 0600 settings document, never in a message (a
//     message may quote a path or a KEY NAME, never a value);
//   - there is an environment channel and it IS read (a catalog row that
//     ignored an exported variable would be the silent failure Rule 15 bans),
//     but it is deliberately NOT republished by config.ApplyEnv — that
//     environment is inherited by every child gopi spawns, so republishing a
//     credential would be a second, wider copy of it. The reasoning is the
//     same sentence DoubaoAPIKeyNotPublishedClaim states, and the catalog
//     rows reuse that constant rather than restating it (Rule 13).
//
// Their PLACE in the precedence is stated once, in cmd/agent/cli_voice_stream.go's
// voiceStreamCredentialSource: a credential FILE named on an explicit channel
// still wins when it exists (the operator's own declaration of where the
// credential lives, and the format this engine has always read); these keys are
// what fill the gap when nobody named one — which is the whole point, because
// the vendor's own directory was until now the only place this channel's
// credential could live, while the batch key beside it had a settings face.
const (
	DoubaoAppIDEnv         = "GOPI_DOUBAO_APP_ID"
	DoubaoAppIDSettingsKey = "doubao_app_id"
	DoubaoTokenEnv         = "GOPI_DOUBAO_TOKEN"
	DoubaoTokenSettingsKey = "doubao_token"
)

// DoubaoResourceIDEnv / DoubaoResourceIDSettingsKey select the doubao
// resource_id explicitly (never left to a client-side default: the official
// SDK's implicit default is a resource this account is refused).
const (
	DoubaoResourceIDEnv         = "GOPI_DOUBAO_RESOURCE_ID"
	DoubaoResourceIDSettingsKey = "doubao_resource_id"
)

// DoubaoHotwordsEnv / DoubaoHotwordsSettingsKey carry comma-separated hotwords
// pushed through request.corpus.context.
const (
	DoubaoHotwordsEnv         = "GOPI_DOUBAO_HOTWORDS"
	DoubaoHotwordsSettingsKey = "doubao_hotwords"
)

// FromSettingsFile loads a Config from a settings.json file. The file may
// contain only a subset of keys (unknown keys are ignored); a missing file is
// not an error (returns an empty Config). This mirrors the old
// deepseek.LoadConfig behaviour ("nil file → nil error, caller falls back"),
// extended to the full knob surface.
func FromSettingsFile(path string) (*Config, error) {
	cfg := &Config{SettingsPath: path}
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil // no settings file → all defaults
		}
		return nil, fmt.Errorf("config: failed to read %s: %w", path, err)
	}
	// Strict=False would be noisy; we decode into a map first so unknown keys
	// (e.g. older files with extra fields) are tolerated silently, then re-marshal
	// the known keys only. Simpler: decode directly — encoding/json ignores
	// unknown fields by default.
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("config: failed to parse %s: %w", path, err)
	}
	// The document's OWN top level, kept once: every later question about what
	// the FILE says (a retired key, an unmodelled engine block) is answered from
	// this, so no caller pays a second read and none can disagree with the
	// decode above. A top level that is not an object is already an error above.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("config: failed to parse %s: %w", path, err)
	}
	cfg.rawTopLevel = raw
	// The nested engine-parameter blocks, decoded by their OWN reader so an
	// unknown block or knob is a NAMEABLE result rather than the silent
	// tolerance encoding/json gives an unknown field (Rule 15). A malformed
	// object is reported here and the document does not load.
	params, unknown, perr := DecodeAudioEngineParam(raw)
	if perr != nil {
		return nil, fmt.Errorf("config: %s: %w", path, perr)
	}
	cfg.AudioEngineParam = params
	cfg.AudioEngineParamUnknown = raw
	cfg.audioEngineParamUnknown = unknown
	// The pre-rename top-level keys fold into the blocks they became. LAST, so
	// the fold sits below every other rung in the precedence (Rule 2) and the
	// new spelling is authoritative wherever both are present.
	cfg.normalizeAudioParams()
	return cfg, nil
}

// SettingsCarriesKey reports whether the settings file this Config was loaded
// from LITERALLY carries key as a top-level JSON key.
//
// WHY IT ASKS THE FILE AND NOT THE DECODED STRUCT: a RETIRED key has no field to
// decode into, and FromSettingsFile deliberately decodes with a plain
// json.Unmarshal (no DisallowUnknownFields) so a file written before a removal
// keeps loading — see Config's own note on the retired merge-tick key. That
// tolerance is a SILENT one, so the only way to tell "the operator still sets a
// retired knob" from "nobody set anything" is to look at the file's own keys.
// The decoded struct cannot answer it; this can.
//
// ONE key per call, and the whole top level is unmarshalled into RawMessage
// values (never into Config), so asking costs a file read and no interpretation:
// a value that no longer parses into a field cannot make this call fail.
//
// A settings file that cannot be read is REPORTED and answered false (Rule 15):
// "I could not tell" must never be presented as "the key is absent". A missing
// file is the ordinary "nothing configured" case and is not reported.
func (c *Config) SettingsCarriesKey(key string) bool {
	if c == nil || key == "" || c.SettingsPath == "" {
		return false
	}
	data, err := os.ReadFile(c.SettingsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false
		}
		fmt.Fprintf(os.Stderr, "config: cannot read %s to check for the retired key %q: %v\n",
			c.SettingsPath, key, err)
		return false
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(data, &keys); err != nil {
		// The file is not a JSON object: FromSettingsFile's own strict decode
		// already reported this to the caller that loaded it, so this probe
		// stays quiet and answers "no key".
		return false
	}
	_, ok := keys[key]
	return ok
}

// settingsPathKey is the catalog Key of the settings.json-path knob. It is
// deliberately NOT a settable settings key (the path locates the file itself),
// so the help renders it as a pointer instead of an editable name.
const settingsPathKey = "settings_path"

// envVarToKey maps an environment variable name to the config JSON field for
// help-generation and CLI-flag→config binding. Nothing here is executed at
// runtime; it exists so one table drives the docs and the CLI.
type envKey struct {
	Env     string
	Key     string
	CLIFlag string
	Help    string
}

// EnvVars is the authoritative catalog of every tunable env var gopi reads,
// with its settings.json key, CLI flag and a one-line help string. The top
// level `gopi -h` and the per-subcommand usage render from (derived) copies
// of this table, so the help can never drift from what the code actually
// honours. The *parsing* of each variable still lives in exactly one place
// (Rule 2); this table only *documents* and *binds* it.
var EnvVars = []envKey{
	// DeepSeek / Anthropic-compatible endpoint
	{"ANTHROPIC_AUTH_TOKEN", "api_key", "api-key", "DeepSeek API key (Claude-compatible endpoint)"},
	{"DEEPSEEK_API_KEY", "api_key", "api-key", "Fallback API key when ANTHROPIC_AUTH_TOKEN unset"},
	{"ANTHROPIC_BASE_URL", "base_url", "base-url", "API base URL (default https://api.deepseek.com)"},
	{"DEEPSEEK_BASE_URL", "base_url", "base-url", "Fallback base URL when ANTHROPIC_BASE_URL unset"},
	{"ANTHROPIC_MODEL", "model", "model", "Model name (default deepseek-v4-pro)"},
	{"DEEPSEEK_MODEL", "model", "model", "Fallback model when ANTHROPIC_MODEL unset"},
	{"GOPI_VISION_MODEL", "vision_model", "vision-model", "Vision model for image understanding (default deepseek-v4-flash-vision-exp)"},
	{VoiceDeviceEnv, VoiceDeviceSettingsKey, "", "Default capture device for `gopi tool voice_input --client` (an id from --list-devices)"},
	{AudioEngineEnv, AudioEngineSettingsKey, "", "`gopi tool voice_input` transcription engine, named for the SHAPE of the work: \"recording\" (Volcengine cloud batch — the audio is uploaded; the DEFAULT), \"streaming\" (Volcengine v2 real-time — partials while you speak, explicit only) or \"local\" (Fun-ASR-Nano, needs the weights on disk; CLI: --engine). The pre-rename name \"GOPI_VOICE_ENGINE\" / settings \"voice_engine\" and the pre-rename values \"doubao\"/\"doubao-stream\" are STILL READ and still honoured; a run that finds one says so on stderr and names the replacement"},
	{AudioVerboseEnv, AudioVerboseSettingsKey, "", "`gopi tool voice_input`'s DEBUG SINK: the streaming channel's per-partial line (one per partial) and its counters (partial count, first/final partial instants, the vendor's logid/reqid, how much audio went out) plus the live-shape note are printed only when this is on. " + AudioVerboseDefaultClaim + ": the transcript and the ordinary per-run lines are what the default path prints, and they are unchanged — this knob adds a stream, it never replaces one. Three channels, in precedence order: the subcommand flag --" + AudioVerboseFlagName + " / -v > this env name > the settings key"},
	{AudioKeyFileEnv, "", "", "File holding a credential for a cloud engine (settings `audio_engine_param.recording.key_file` / `audio_engine_param.streaming.key_file`; CLI: --doubao-key-file). Default ~/.doubao/apikey.txt (the LEGACY location — kept working, no longer the only place a credential can live). ONE file knob, TWO formats: the recording engine reads the single-line API key, the streaming engine reads the KEY=VALUE APPID/TOKEN document. An explicitly named file outranks an inline value"},
	{AudioAPIKeyEnv, "", "", "The recording engine's API key INLINE — `audio_engine_param.recording.api_key` — the rung between the rung between an explicitly named key FILE and the ~/.doubao/apikey.txt fallback. " + DoubaoAPIKeyNotPublishedClaim + " (a 0600 settings file is the narrow channel; the process environment is inherited by every child gopi spawns)"},
	{AudioAPIBaseURLEnv, "", "", "The recording engine's endpoint root (`audio_engine_param.recording.api_base_url`), from which BOTH of its URLs derive (one root, so submit and query can never be pointed at two hosts; CLI: --doubao-base-url; --doubao-submit-url / --doubao-query-url name one URL each and are more specific). Default https://openspeech.bytedance.com/api/v3/auc/bigmodel"},
	{AudioResourceIDEnv, "", "", "The recording engine's X-Api-Resource-Id (`audio_engine_param.recording.resource_id`; default volc.seedasr.auc); never left to a client-side default. Three channels, in precedence order: CLI: --doubao-resource > the env name here > this nested settings key"},
	{AudioHotwordsEnv, "", "", "Comma-separated hotwords for the recording engine (`audio_engine_param.recording.hotwords`, sent as request.corpus.context). Three channels, in precedence order: CLI: --doubao-hotwords > the env name here > this nested settings key"},
	{AudioStreamEndpointEnv, "", "", "The streaming engine's endpoint (`audio_engine_param.streaming.endpoint`) — the wss:// URL (CLI: --doubao-stream-endpoint); the same block carries `audio_engine_param.streaming.cluster` (the product line the vendor authorises and bills by, default volcengine_streaming_common). Default wss://openspeech.bytedance.com/api/v2/asr; until 2026-10-09 it was settable on no channel at all"},
	{AudioAppIDEnv, "", "", "The cloud streaming engines' APPID, inline at `audio_engine_param.streaming.app_id` (v2) and `audio_engine_param.sauc.app_id` (v3 SAUC) — ONE env name for the ONE application id the vendor issues per account. " + DoubaoAPIKeyNotPublishedClaim + ". A credential FILE named on an explicit channel still wins when it exists"},
	{AudioTokenEnv, "", "", "The TOKEN (the second half of the same credential PAIR) for both cloud streaming engines — `audio_engine_param.streaming.token` and `audio_engine_param.sauc.token`; same discipline as the APPID beside it. " + DoubaoAPIKeyNotPublishedClaim + ". A credential FILE named on an explicit channel still wins when it exists"},
	{AudioLocalModelsEnv, "", "", "The LOCAL engine's weights directory (`audio_engine_param.local.models_dir`) — its first settings/env surface (until 2026-10-09 only --models existed, so a headless deployment had to spell the path every invocation). A path named here is EXPLICIT: the two-stage deployment search may not override it"},
	{AudioLocalRuntimeEnv, "", "", "The LOCAL engine's runtime executable (`audio_engine_param.local.runtime_bin`, llama-funasr-cli; CLI: --runtime) — new surface, same reason and same explicitness contract as the weights directory beside it"},
	{"GOPI_VISION_BASE_URL", "vision_base_url", "vision-base-url", "Vision API base URL (default https://api.deepseek.com/anthropic)"},

	// `gopi tool tokens --compare` measurement endpoint. It is not a model and
	// not a switch: it is where the tool asks what the SERVER counts for a
	// request body, so a local renderer can be held to the server's own
	// arithmetic rather than to a second local guess. Unset = the tool reports
	// that it has no server number to compare against (Rule 15: no silent
	// fallback), and `--body` still prints the local count.
	{TokensCountURLEnv, "", "",
		"`gopi tool tokens --compare`'s count_tokens endpoint: an Anthropic-compatible " +
			"`/v1/messages/count_tokens` on the host the measured requests go to. It answers " +
			"with the SERVER's own input_tokens for the exact bytes sent, which is the ground " +
			"truth the tool exists to measure against. Unset/empty = no server number, reported " +
			"as such — the tool never substitutes a local estimate for the server's count"},
	{TokensCountKeyEnv, "", "",
		"The credential the " + TokensCountURLEnv + " endpoint is called with. " +
			DoubaoAPIKeyNotPublishedClaim + " (a credential in settings.json is the narrow " +
			"channel; the process environment is inherited by every child gopi spawns). " +
			"An empty key is passed as an empty Authorization header, so a local endpoint " +
			"that needs no credential works with neither set"},

	// Reply language (operator-requested: type in English, be answered in
	// Chinese). A VALUE knob, not a switch: absent or empty = no directive at
	// all (the system prompt is then byte-identical to one built without this
	// feature). The value is a language tag ("zh-CN", "en-GB"); an unusable
	// value is reported on stderr and the next channel decides. Scope is the
	// REPLY only — the model's reasoning language is untouched.
	{OutputLanguageEnv, OutputLanguageSettingsKey, OutputLanguageFlagName, "Language of gopi's REPLIES — a " +
		OutputLanguageValueQualifier + " " + OutputLanguageValueShape + " (" + OutputLanguageExampleTags + "; " +
		OutputLanguageNotAValueClaim + "): set it and a directive is prepended to the system prompt of both the " +
		"task path and `gopi chat`; unset/empty emits nothing. " + OutputLanguageScopeClaim},

	// Memory prefetch (typing-time, read-only, default ON)
	{MemoryPrefetchEnv, MemoryPrefetchSettingsKey, MemoryPrefetchFlagName, "Typing-time MEMORY PREFETCH " +
		"(" + MemoryPrefetchDefaultClaim + "): while you type in `gopi chat`, a READ-ONLY background fetch runs a debounced memory " +
		"retrieval and, on hits, appends a labelled RAG section to the turn's prompt. OFF starts no goroutine " +
		"and leaves the prompt byte-identical; one knob, three channels (flag > env > settings). Each prefetch " +
		"run leaves a record in the session directory (" + MemoryPrefetchRecordName + ") — Rule 35: no new " +
		"persistent artifact outside the session dir that already exists"},

	// The A-class width reading. User-facing: box-drawing (U+2500-257F), the
	// turn marker U+258C and every other East Asian Width AMBIGUOUS rune are two
	// cells in a CJK terminal and one in a Western one, and this is where gopi is
	// told which of the two it is drawing into. Empty (the default) lets the
	// LOCALE decide.
	{AmbiguousWidthEnv, AmbiguousWidthSettingsKey, AmbiguousWidthFlagName,
		"How East Asian Width AMBIGUOUS (A) runes are measured — box-drawing " +
			"(U+2500-257F), the turn marker, em dashes, arrows, enclosed numbers: " +
			AmbiguousWidthWide + " (two cells, a CJK terminal), " + AmbiguousWidthNarrow +
			" (one cell, a Western one), or unset = the LOCALE decides " +
			"(LC_ALL, then LC_CTYPE, then LANG: zh/ja/ko wide, else narrow). It is a " +
			"REAL rendering property, measured on this host: the same character is two " +
			"cells under a CJK font and one under xterm's default face, so no single " +
			"answer is right for every terminal and the knob names the choice instead " +
			"of guessing. An unusable value is reported and the next channel decides " +
			"(Rule 15); see devdoc/lineedit-eaw-ambiguous.md for the measurement"},

	// The chat surface's IDEAL GEOMETRY. User-facing: it decides the width the
	// startup screen, the tips panel and the info bar are drawn for, and a
	// terminal narrower than it is TOLD what it lost (never silently degraded).
	{ChatIdealGeometryEnv, ChatIdealGeometrySettingsKey, ChatIdealGeometryFlagName,
		"The chat surface's IDEAL GEOMETRY: the COLS" + ChatIdealGeometryDimSep + "ROWS size its chrome " +
			"(startup screen box + tips panel, the reserved editing line and status row, the info bar) is " +
			"designed for, default " + chatIdealGeometryDefault + ". " + chatIdealGeometryEffectClaim +
			"; when the terminal is smaller, the surface says what it cannot draw (no silent degradation). " +
			"One knob, three channels (flag > env > settings), resolved at ONE site " +
			"(config.ChatIdealGeometry) that BOTH the normal chat and the replay/beat filmer read"},

	// The chat_surface tool's SELF-DRIVEN TURN BUDGET. User-facing: it decides
	// how long one autonomous stretch may run before the loop refuses and says
	// so on the console the operator is watching.
	//
	// THIS row carries the full claim (the top-level index carries the one-line
	// summary): `gopi --help-env` is the complete surface, and the sentence that
	// says what the bound DOES belongs where there is room for it.
	{ChatSurfaceTurnsEnv, ChatSurfaceTurnsSettingsKey, ChatSurfaceTurnsFlagName,
		ChatSurfaceTurnsRowSummary + "; " + chatSurfaceTurnsEffectClaim},

	// The terminal a FILM is photographed in. User-facing: it decides which
	// emulator the operator sees in the animation, so a film can be made in the
	// terminal they actually use. The NAME is validated against internal/shot's
	// registry — the usable names and the built-in default are THAT table's facts
	// (this package does not import it, so they are not restated here as a second
	// definition); an unknown or unusable name is refused with the reason and the
	// usable list (Rule 15).
	{FilmTerminalEnv, FilmTerminalSettingsKey, FilmTerminalFlagName,
		"Which terminal `gopi chat --film` photographs, by NAME — the usable names are the film " +
			"driver's terminal registry (printed by every refusal, and on `gopi chat -h`). Resolved by " +
			"ONE function, shot.ResolveFilmTerminal (flag > env > settings > the registry's built-in " +
			"default; Rule 2/34); an unknown name is refused with the reason and the usable list, never " +
			"silently replaced by the default"},

	// Input framing
	{PromptEnvVar, "prompt", "prompt", "Task instruction (--prompt; may be a file ref: --prompt @task.md). When set, positional args are input files/images. CAP: published into GOPI_PROMPT only up to the kernel's per-string execve limit (config.ExecveMaxStringBytes = 131072 bytes, MAX_ARG_STRLEN, counted as NAME=value+NUL); above it the variable is NOT published (one such string makes every child fail E2BIG — git rev-parse, sh for notify.sh), the prompt is carried in-process instead and the skip is announced with both byte counts"},

	// Settings path
	{"GOPI_HOME", "gopi_home", "home", "Gopi data root: ~/.gopi becomes $GOPI_HOME/.gopi (test/sandbox isolation)"},
	{SandboxNestedProfileOptOutEnv, "", "", "Allow a GOPI_HOME that names a PROFILE (`<home>/.gopi`, which resolves to `<home>/.gopi/.gopi` — a second, nested profile holding its own ledger, sessions and plugins that no reader of the profile you meant ever sees). Refused by default, naming `GOPI_HOME=<parent>`; setting this ANNOUNCES the degradation and proceeds"},
	{"GOPI_SETTINGS", settingsPathKey, "settings", "Path to settings.json"},

	// Task-row synchronization (epic: five conflated axes). Default OFF.
	//
	// IT NO LONGER GATES A WORKING PROTOCOL, and this comment says so because
	// the old one promised the opposite: the MANUAL verbs it named
	// (`task sync|diff|harvest --peer`, `task push`) were DELETED 2026-11-05
	// (cmd/agent/task_sync_verb_removal.go), and the AUTOMATIC crossings it
	// gates all speak to a peer through legs that are now RETIRED
	// (internal/tools/remote_leg_retired.go): a claim broadcast, a terminal
	// push, a serve-tick delta pass and a submit forward each refuse LOCALLY
	// and immediately, naming the removal, without opening a connection.
	//
	// SO WHAT DOES ON DO TODAY? It turns those refusals ON — each one is then
	// REPORTED (the submit path refuses the row, the worker's end hook logs
	// `terminal push: FAILED`) where OFF makes the same crossings no-ops that
	// report nothing. That is a real difference and it is the opposite of what
	// a "sync switch" suggests, which is why the row says it plainly instead
	// of implying a working protocol behind a flag.
	//
	// Independent of role (identity) and of memory_peers (knowledge assets),
	// which are never gated by it.
	{TaskSyncEnv, TaskSyncSettingsKey, "", "Task-row AUTOMATIC exchange with sync_peers peers: absent or false = OFF (default — the automatic crossings are no-ops). ON is NOT a working cross-node protocol any more: the peer legs the crossings used (claim broadcast / terminal push / serve delta pass / submit forward) were RETIRED 2026-11-05 and the manual verbs they replaced (`task sync|diff|harvest --peer`, `task push`) are DELETED, so ON makes each crossing refuse LOCALLY and report the refusal (a client-node submit is refused; the end hook logs a failed terminal push) instead of being skipped silently. A row that must run on another host is FILED there — `gopi task submit` on that host (Rule 28 / Rule 40)"},
	{ChatOpAutoEnv, ChatOpAutoSettingsKey, "", "Chat console's standing posture for the ONE class it must ask about — a LONG operation — judged by the CATEGORY of the action, never by a clock: absent or false = ALL-ASK (default; an unauthorised whole-tree suite / full build / package install / merge|worker|serve sweep is asked about), true = the console may also start an unauthorised long operation itself. The other two classes are NOT this knob's business: OPERATOR WORK (ops, DevOps, network, script, document, query, diagnosis, bug row, housekeeping, short job) always runs with no permission question, and a REPOSITORY WRITE (source edit, the gate run over that change, a stage/commit) is always refused and routed to `task submit` — no posture opens it. Classified by tools.ChatOpClassify, decided by tools.ChatOpAdmit; both vocabularies are exported so the prompt renders the same words. Resolved by tools.ChatOpChannel — one reader, and the refusal names which channel decided"},

	// Thinking / reasoning
	{"GOPI_THINKING", "thinking", "thinking", "Thinking mode: disabled (default) or enabled"},
	{"GOPI_REASONING_EFFORT", "reasoning_effort", "reasoning-effort", "Reasoning depth, validated against the settings.json effort_values list (default minimal|low|medium|high|max)"},
	{"GOPI_EFFORT_VALUES", "effort_values", "", "Comma-separated legal effort list (settings key effort_values; overrides the baseline minimal|low|medium|high|max)"},
	{"GOPI_THINKING_BUDGET", "thinking_budget", "thinking-budget", "Thinking budget in tokens per round (default 1024)"},
	{"GOPI_HIDE_THINKING", "hide_thinking", "hide-thinking", "Set to 1 to hide live reasoning display"},
	{"GOPI_KEEP_THINKING", "keep_thinking", "keep-thinking", "Set to 1 to replay reasoning in assistant history (A/B control; default strips it)"},
	{"GOPI_MAX_TOKENS", "max_tokens", "max-tokens", "Per-response output cap (default 16384)"},
	{"GOPI_CONTEXT_WINDOW", "context_window", "context-window", "Context window (default 200000)"},

	// Runtime
	{"GOPI_TIMEOUT_SEC", "timeout", "timeout", "Overall agent timeout in seconds (0 = unlimited)"},
	{"GOPI_COMPACT", "compact", "compact", "Memory compaction: \"off\" disables (default on)"},
	// THE OPT-IN for the whole automatic reduction path (fold → compact → trim).
	// It has no settings.json key and no CLI flag on purpose: a run that wants a
	// filled window SHORTENED says so in its own environment, once
	// (internal/agent/context_pressure.go). Unset = warn, remove nothing.
	{"GOPI_CONTEXT_AUTOREDUCE", "", "", "Set to 1 to re-enable automatic context reduction (default off: a full window WARNS and removes nothing)"},
	{"GOPI_DEBUG", "debug", "debug", "Set to 1 to enable debug logging"},
	// THE RUN-LOG CONFIGURATION DUMP (GOPI_DEBUG_CONFIG). No settings key and no
	// CLI flag on purpose: the thing gated is "THIS run's log", not "this
	// PROJECT's behaviour" — a settings key would turn every run of that profile
	// into a dumping run, and the question it answers ("what did the run I just
	// started send?") is per-invocation. It reuses the report `gopi /config`
	// already renders, so the knob adds no second renderer (debug_config_dump.go
	// in internal/config owns the switch, cmd/agent's start-up seam owns the
	// write site).
	{DebugConfigDumpEnv, "", "", "Set to 1 to write this run's configuration in force (the same table `gopi /config` shows) into its own task output log, before the first model request"},

	// Compaction parameters
	{"GOPI_COMPACT_TRIGGER", "compact_trigger", "compact-trigger", "Compaction trigger ratio (default 0.75 — internal/agent/context.go's DefaultCompactTriggerRatio)"},
	{"GOPI_COMPACT_TARGET", "compact_target", "compact-target", "Compaction target ratio (default 0.35 — internal/agent/compact.go's DefaultCompactTargetRatio)"},
	{"GOPI_COMPACT_KEEP_RECENT", "compact_keep_recent", "compact-keep-recent", "Recent messages never compacted (default 8)"},
	{"GOPI_TURN_FOLD_KEEP_TURNS", "", "", "Completed turns the turn fold keeps VERBATIM as an operator escape hatch (default 0 — internal/agent/turnfold.go's turnFoldRecentTurnsDefault, i.e. no window: every completed turn is replaced by its [IMPORTANT] note); a value above 0 keeps that many newest turns raw"},
	{"GOPI_COMPACT_MAX_SUMMARY_TOKENS", "compact_max_summary_tokens", "compact-max-summary-tokens", "Summarizer output budget"},
	{"GOPI_COMPACT_COOLDOWN", "compact_cooldown", "compact-cooldown", "Pause after summarizer failure (seconds, default 600)"},
	{"GOPI_COMPACT_REFERENCE_ONLY", "compact_reference_only", "compact-reference-only", "Wrap memory in REFERENCE ONLY (default on)"},

	// Logging
	{"GOPI_RAW_LOG", "raw_log", "raw-log", "Raw HTTP log: on (default) or off"},
	{"GOPI_RAW_LOG_DIR", "raw_log_dir", "raw-log-dir", "Raw log directory (default ~/.gopi/raw)"},

	// Tools
	{"GOPI_TOOL_TIMEOUT_SEC", "tool_timeout_sec", "tool-timeout", "read/write/edit timeout (seconds, default 30)"},
	{"GOPI_BASH_TIMEOUT_SEC", "bash_timeout_sec", "bash-timeout", "bash absolute timeout (seconds, default 120)"},
	{"GOPI_BASH_IDLE_SEC", "bash_idle_sec", "bash-idle", "bash no-output timeout (seconds, default 120)"},
	{"GOPI_ALLOW_DANGEROUS", "allow_dangerous", "allow-dangerous", "Set to 1 to allow dangerous commands"},
	{WebSearchScriptsDirEnvName, "websearch_scripts_dir", "websearch-scripts-dir", "Web search scripts directory"},
	{"GOPI_MEMORY_DB", "memory_db", "db", "Knowledge base path (default ~/.gopi/memory.db). Ranked chain: the ledger flag of the verb that owns one (`gopi memory search … --db X`) > this env var > GOPI_HIPPO_DB > settings.json memory_db > the default; a LEADING `--db` is a RUN flag, not this knob"},
	{TmpDirEnvName, TmpDirSettingsKey, TmpDirFlagName, "Temp workspace for gopi and EVERY child it spawns (default <home>/workspace/_tmp). A caller-exported TMPDIR is honoured as the ambient override; an unusable value fails loudly naming path + reason"},
	// The temp workspace's HEADROOM FLOOR (GOPI_TMPDIR_HEADROOM_BYTES). Its
	// environment name and its default live in internal/scratch beside the ONE
	// resolver (scratch.TmpDirHeadroomFloor), referenced here rather than typed
	// (Rule 1b/13/14) — the same shape as the scratch sweep's rows above. It has
	// no settings key and no CLI flag — the criterion is a host fact an operator
	// states for one run — so the catalog renders the em dash in both of those
	// columns, exactly as `gopi --help-env` prints it. Without this row the knob
	// was absent from `gopi --help-env` while CONFIG.md told an operator to set
	// it (measured 2026-10-31: TestConfigDocFlagColumnReportsRowsAbsentFromCatalog
	// named CONFIG.md:558 GOPI_TMPDIR_HEADROOM_BYTES).
	{scratch.TmpDirHeadroomFloorEnvName, "", "", "Free-space FLOOR in BYTES the resolved temp root is measured against (statfs on the partition holding it; default 2147483648 = 2 GiB, scratch.TmpDirHeadroomFloorBytes). LOUD, never fatal: the warning names path + device + free bytes + the reclaim candidates, all report-only — nothing is deleted. A malformed value is REPORTED and falls back to the default, never read as 0 (which would disable the criterion silently)"},
	// Sandbox: the two knobs of the ONE standard private profile
	// (`gopi tool sandbox create` — the root, and the optional ledger seed). Their
	// constants live in sandbox.go beside the one resolver; the rows are here
	// because this table is what `gopi --help-env` renders and what the flag
	// ⇒ env map is built from, so an uncatalogued knob has no help face and no
	// env-named flag reach (the defect these two rows close).
	{SandboxEnvName, SandboxSettingsKey, SandboxFlagName, "Root under which agent sandboxes live — the ONE standard private gopi profile (`gopi tool sandbox create`). A sandbox IS a GOPI_HOME: its profile is <sandbox>/.gopi. Default <home>/workspace/gopi-sandbox; no ambient channel (nothing outside gopi reads a sandbox root)"},
	{SandboxLedgerSeedEnvName, "", "", "Database COPIED to become a new sandbox's ledger (<sandbox>/.gopi/memory.db, private and writable — never a link and never the seed itself). Unset ⇒ the ledger starts empty. A seed that is missing, is not a SQLite database, or lives INSIDE the sandbox refuses the create, loudly and by name"},
	{"GOPI_NO_ENFORCE_MEMORY", "no_enforce_memory", "no-enforce-memory", "Disable harness memory checkpoints (task-start search / task-end store) — A/B isolation"},
	{"GOPI_NO_ENFORCE_MEMORY_CONSULT", "no_enforce_memory_consult", "no-enforce-memory-consult", "Disable the review-time memory-consult checkpoint (\"which work could a search have saved?\") — measurement instrument off"},
	{"GOPI_TEST_SCRATCH_GC_MIN", "scratch_gc", "scratch-gc", "Resident scratch sweep cadence in minutes (worker/merger tick; 0 or --scratch-gc=false disables)"},
	{scratch.GCStaleAgeEnvName, scratch.GCStaleAgeSettingsKey, ScratchGCStaleMinFlagName, "Scratch sweep STALE WINDOW in minutes: an unheld scratch entry older than this is garbage (default 360 = 6h; a held entry is never collected)"},
	{scratch.HolderScanDeadlineEnvName, "", "", "Wall-clock bound in SECONDS on ONE /proc holder scan (scratch.LiveHolders / scratch.ScanHolders): cwd + exe + every fd + a maps file per process. Default 8 (scratch.HolderScanDefaultBudget). 0 or any non-positive value is the deliberate UNBOUNDED form; a malformed value is reported and falls back to the default. Past the bound the scan stops before the next process and reports Truncated — every DELETE caller then keeps the candidate (fail-closed), and the report says the scan was truncated instead of claiming a holder it never saw"},
	{scratch.ToolchainCacheMaxEnvName, "", "", "Size bound in GiB of the AGENT-side Go toolchain cache (the cache `go` itself fills; the DIRECTORY is resolved with the toolchain's own precedence — `--path` > `$GOCACHE` > GOCACHE in the go env file `~/.config/go/env` > `<user cache dir>/go-build`), as opposed to the merge gate's own per-repo cache. `gopi tool scratch-gc gocache` measures the cache BODY (the two-hex entry shards; never the toolchain's README/trim.txt/log.txt/CACHEDIR.TAG) against it, and `--trim` evicts whole shards OLDEST FIRST down to it, keeping any shard whose newest entry is inside scratch.ToolchainCacheEvictMinAge (24h). The invocation's own --max-gib outranks this. Measured subject (this operator's host, 2026-10-12): 119.95 GiB held, on /dev/nvme0n1p7 (device 66309, /home) at 96% full while / (device 66307) had 131 GiB free — the measurement the off-/ placement test cannot make. Malformed/non-positive ⇒ reported, then the default 25 (scratch.DefaultToolchainCacheMaxBytes)"},
	// (MergeTickIntervalEnvName / `merge_tick` USED to be catalogued here. The
	// RESIDENT MERGE TICK is RETIRED 2026-09-27 — the worker must not merge,
	// `gopi merge` does — so this row is gone rather than left advertising a knob
	// nothing honours. The retired spellings survive in internal/config as
	// MergeTickRetiredEnvName / MergeTickRetiredSettingsKey, which the
	// retired-knob refusal (cmd/agent/retired_tick_knobs.go) NAMES back at an
	// operator still setting them.)
	// (MemorySyncTickIntervalEnvName / `memory_sync_tick` USED to be catalogued
	// here. The RESIDENT MEMORY-SYNC TICK is RETIRED 2026-10-25 — a round that
	// spawned one cross-node ledger-sync child per peer blocked the worker for
	// 315-534s, and unattended convergence is now the operator's own decision — so
	// this row is gone rather than left advertising a knob nothing honours. The
	// retired spellings survive in internal/config as
	// MemorySyncTickRetiredEnvName / MemorySyncTickRetiredSettingsKey, which the
	// retired-knob report (cmd/agent/memory_sync_tick_retired.go) NAMES back at an
	// operator still setting them. The STORE-triggered round below is a different
	// lifecycle: it drove its own child, not that tick.)
	// (MemorySyncEveryEnvName / GOPI_MEMORY_SYNC_EVERY_SEC and the worker-pull
	// switch's TaskPullEnv / `task_pull` USED to be catalogued here. Both lost
	// their last reader on 2026-11-05/06 — the cross-node row family (batch 1)
	// deleted `gopi task receive --takeover`, which was the pull switch's only
	// consumer, and `gopi memory sync` went with it, taking the `--every` loop
	// this cadence drove. Batch 5 (d7d836e5) removed the rows rather than leave
	// them advertising knobs nothing honours. The retired spellings survive in
	// internal/config as TaskPullRetiredEnvName / TaskPullRetiredSettingsKey /
	// MemorySyncEveryRetiredEnvName, which the retired-knob report
	// (cmd/agent/task_pull_retired.go, cmd/agent/memory_sync_every_retired.go)
	// NAMES back at an operator still setting them — and neither is given a
	// settings key it never had.)
	{MemorySyncOnStoreEnv, MemorySyncOnStoreSettingsKey, "", "RETIRED 2026-10-27: the store-triggered memory-asset sync was DELETED, so a successful memory_store writes this node's /home/user/.gopi/memory.db and reaches no peer. The values push | pull | off are all still ACCEPTED and all now mean off (only a malformed value is an error, never a silent fallback), and an operator still setting push or pull is told on the start path. Cross-node propagation is a manual ONE-WAY COPY: gopi memory backup ships this ledger to a declared memory_peers peer and imports nothing from it, so the two knowledge bases stay divergent"},

	// Harness nudge / dead-end trim tuning (loop.go; env-overridable for
	// tests, constants are the defaults — Rule 2).
	{"GOPI_NUDGE_ROUNDS", "", "", "Rounds without a task list before the harness nudges (default 5)"},
	{"GOPI_NUDGE_TOOLS", "", "", "Tool rounds without a task list before the harness nudges (default 3)"},

	// Distributed task ledger (task/sync/worker/role families — M4.x):
	// topology + serve-tick pacing + worker loop knobs, all read in exactly
	// one place each (config.go / cmd/agent cli_tasks_*.go).
	{"GOPI_NODE_ROLE", "role", "", "Node identity: server (dispatcher) or client (compute node); overrides settings.json role"},
	// The CLONE WORKSPACE CHANNEL (2026-10-18): dev_repo is the SWITCH (non-empty
	// = `gopi worker` clones its workspace from there; empty/absent = today's
	// worktree channel, unchanged) and workspace_root is the reclaimable parent
	// the per-task clone lands under. One decision each — no third boolean.
	{DevRepoEnvName, DevRepoSettingsKey, "", "host:path of the repository a task WORKSPACE is cloned from (same spelling as a repo entry's path, e.g. peer:~/work/gopi). NON-EMPTY IS THE SWITCH: empty/absent = the worktree channel, unchanged; set = `gopi worker` clones here, runs pull --rebase → build → push, and reclaims the workspace when the push lands"},
	{DevRepoEnvName, DevRepoSettingsKey, "", "the repository this host's runs work in, as the OPTIONAL third source of the run-repository chain (the row's own deliverable.repo → the current working directory's git repo → this value; `gopi config show` prints the answer). Empty/absent is NORMAL: a run started inside any git repository works. A `host:path` spelling (e.g. peer:~/work/gopi) additionally turns ON the clone workspace channel, where `gopi worker` clones its workspace from here, runs pull --rebase → build → push, and reclaims the workspace when the push lands"},
	{WorkspaceRootEnvName, WorkspaceRootSettingsKey, "", "Reclaimable PARENT directory the clone channel opens one per-task subdirectory in (default <home>/workspace — the same root the worktree channel uses). The subdirectory is deleted once the task's push lands; a run that exhausts its retries KEEPS it and says so"},
	{"GOPI_SYNC_TO", "sync_to", "", "ssh alias of the node this client reports/syncs to (server side stays empty)"},
	{"GOPI_SYNC_PEERS", "sync_peers", "", "Comma-separated TASK-LEDGER peer whitelist (ssh aliases; overrides settings.json sync_peers). Empty = single-node task mode"},
	{"GOPI_MEMORY_PEERS", "memory_peers", "", "Comma-separated KNOWLEDGE-ASSET peer whitelist (ssh aliases `gopi memory backup --peer` will ship a ledger copy to; overrides settings.json memory_peers). Decoupled from sync_peers so the knowledge channel survives single-node task mode"},
	{MemorySyncTimeoutEnv, "", "", "The bound of ONE cross-node asset-sync ssh leg, in seconds (default 900). BOTH faces are live again since 2026-11-06: `gopi memory sync --timeout N` publishes into it and the flag wins, so an exported value is the fallback. One reader: assetSyncSSHBound (internal/tools/memory_sync_assets.go). The peer applies every row before answering, so the bound must exceed its apply time (measured 2026-09-26: 5209 rows / 47.5 s)"},
	{"GOPI_SYNC_NODE", "", "", "Node identity override for sync self-tagging (same-host two-node simulations; default hostname)"},
	{"GOPI_ADOPTION_INTERVAL", "", "", "Serve-tick adoption sweep interval in seconds (default 900; 0/negative = disabled)"},
	{"GOPI_WORKER_MAX_ROUNDS", "", "", "Worker loop round cap (0 = unlimited, drain-then-exit)"},
	{"GOPI_WORKER_IDLE_EXITS", "", "", "Worker self-exit after N consecutive empty-queue rounds (default 3; 0 = wait forever)"},
	{"GOPI_TASK_NOTIFY_WEBHOOK", "notify.webhook", "", "Task-notify webhook URL (HTTP POST on task transitions; settings key notify.webhook)"},
	{"GOPI_TASK_NOTIFY_CMD", "notify.cmd", "", "Task-notify shell command (run on task transitions; settings key notify.cmd)"},
	{"GOPI_TASK_NOTIFY_EVENTS", "notify.events", "", "Task-notify event set (default claimed,in_progress,completed,failed,superseded; \"all\" = every status; settings key notify.events)"},
	{"GOPI_TASK_NOTIFY_PATTERN", "notify.pattern", "", "Task-notify event filter (e.g. task id / tag pattern; settings key notify.pattern)"},
	{"GOPI_TASK_NOTIFY_ASYNC", "notify.async", "", "Task-notify delivery mode: async fire-and-forget vs blocking (Set to 1; settings key notify.async)"},
	{TaskStallSecEnv, TaskStallSecSettingsKey, "", "Stale-REPORT grace in seconds: an in_progress row whose heartbeat is older than this is reported as stale to the notify sink (default 1800 = 30min; 0 disables detection). It reports only — gopi no longer disposes stale rows (devdoc/zombie-sweep-removed.md)"},
	{TaskVerifyTimeoutEnv, TaskVerifyTimeoutSettingsKey, "", "Wall-clock cap in seconds on the child `gopi task verify` spawns (default 1800 = 30min; 0 = unlimited, the explicit opt-out). CLI: `gopi task verify --timeout N`, which wins. A timed-out verify records NO verdict: the caller gets a machine-readable timeout line and a distinct exit code"},
	{TaskDeliveryProofAllowEnv, "", "", "Operator's escape from the close-out DELIVERY PROOF: truthy releases a `completed` writeback whose delivery claim could not be measured, and the release is recorded on the row's trail as `delivery-proof-unverified` (`gopi task show` renders it) — the measurement still runs, so \"proven\" and \"let through by decision\" stay distinguishable. The typed twin is `gopi task update <row> --force`; both faces are ONE decision (tools.deliveryProofAllowanceFor), and neither can be inherited by the harness writeback or the merger (the task-lifecycle tick it used to name was deleted 2026-10-30, task 01a18ef7). Absent or false = OFF (default: an unprovable delivery claim refuses the write)"},
	{SkipClaimBroadcastEnv, "", "", "Set to 1 to skip the claim broadcast after a task claim"},
	{WorkerIdleIntervalEnv, "", "", "Drained-queue re-check cadence in seconds for `gopi worker` (default 30; `--once` exits instead of waiting). Absent/0/negative = the default; a malformed value is reported and the default kept"},
	{WorktreeSweepMinAgeEnvName, "", "", "Age threshold in HOURS below which `gopi task sweep-worktrees` never reclaims a registration: `gopi task sweep-worktrees --min-age-h N` (default 24; a malformed value is reported on stderr and the default kept). The threshold is measured on git's own registration record, never the directory mtime, and the holder pass protects anything younger that a live process or a non-terminal row still claims"},
	{WorktreeSweepMaxEnvName, "", "", "Cap on how many registrations ONE `gopi task sweep-worktrees` run reclaims — the blast-radius throttle: `gopi task sweep-worktrees --max N` (default 50; a malformed value is reported on stderr and the default kept)"},
	{BrewIntervalEnv, "", "", "Pace of the session-exit `gopi memory brew` trigger, in seconds (default 86400 = 24h; `0` disables the trigger entirely). `gopi memory brew -h` advertises this knob"},

	// Tools / subcommand tuning.
	{"GOPI_BASH_LOCALE", "", "", "Locale for bash tool child processes (default C)"},
	// The chat run's own surface identity, published to its children at arm time
	// so a `gopi ui_state --target chat` a child runs claims THIS run's dump
	// instead of guessing among the profile's dumps. It has no settings-file key
	// and no real default ("" = nothing published, and the reader then uses the
	// general rule), which is why its two middle fields are empty — the same
	// shape every process-scoped channel in this table takes. Read by
	// ReadChatSurfaceIdentity (chat_surface_identity.go).
	{ChatSurfaceIdentityEnv, "", "", "This chat run's own surface identity, published to its children (`<pid|self>:<session-dir>`; empty = none published)"},
	{"GOPI_SHOT_CHROME", "", "", "Chromium binary path for `gopi tool shot` (default: playwright cache / PATH)"},
	{"GOPI_CODE_SEARCH_ROOT", "", "", "code-search index root directory"},
	{"GOPI_WEB_ADDR", "", "", "`gopi web` listen address (default localhost:PORT; a non-loopback bind is token-gated)"},
	{"GOPI_WEB_TOKEN", "", "", "Pin the `gopi web` LAN access token (default: a random per-run token)"},
	{"GOPI_WEB_TOOLS", "", "", "Enable `gopi web` / `gopi ui` agent tools (any non-empty value)"},
	{"GOPI_ERROR_DUMP_DIR", "", "", "Directory for deepseek error-path dumps (debugging)"},
	{"GOPI_1214_DUMP_DIR", "", "", "Directory for error-1214 degradation dumps (debugging)"},
	{"GOPI_ANCHOR_GUARD", "", "", "Set to 0 to disable the memsync anchor guard"},
	{"GOPI_HIPPO_DB", "", "", "Legacy alias for the knowledge base path (superseded by GOPI_MEMORY_DB / --db)"},
}

// normalizedBoolValue cleans a raw "true"/"false"/"1"/"0"/"off"/"on" string.
// The tri-state is the CLI/settings channel's contract (a value that is neither
// an ON nor an OFF spelling is UNKNOWN, not false), while the SPELLINGS come
// from the ONE definition in internal/boolvocab (Rule 14) — this function is a
// reader of that vocabulary, never a copy of it (Rule 13).
func normalizedBoolValue(s string) (bool, bool) {
	switch {
	case boolvocab.IsTrue(s):
		return true, true
	case boolvocab.IsFalse(s):
		return false, true
	}
	return false, false
}

// CLI flags declared via the EnvVars catalog are boolean switches when the
// underlying config field is a bool (or pointer bool). isBoolKey drives both
// the flag parser (optional value) and the docs renderer.
func isBoolKey(key string) bool {
	switch key {
	case "thinking", "hide_thinking", "keep_thinking", "compact", "debug", "allow_dangerous",
		"compact_reference_only", "no_enforce_memory", "no_enforce_memory_consult", "scratch_gc",
		MemoryPrefetchSettingsKey:
		return true
	}
	return false
}

// defaultEffortValues is the effort value whitelist used when the settings
// file does NOT declare its own "effort_values" list — i.e. the DeepSeek /
// Anthropic-compatible baseline (verified live 2026-08-22: all five values are
// accepted with HTTP 200; the endpoint ignores the field, so "accepted" is the
// whole of what this list can promise — see devdoc/thinking-budget.md §1).
// "off" is a harness-only resolution value
// (it means thinking disabled, never a wire value) and is always accepted on
// top of the list; it is deliberately NOT a member here.
var defaultEffortValues = []string{"minimal", "low", "medium", "high", "max"}

// DefaultEffortValues hands the baseline whitelist above to a reader OUTSIDE this
// package WITHOUT a second copy of it (Rules 13/14).
//
// It exists because a second surface has to answer the same question the resolver
// answers: `/config`'s `effort_values` row must name the list that is applied when
// nothing declares one — that list is exactly the one `resolveReasoningEffort`
// falls back to, so a literal typed in cmd/agent would start drifting from the
// behaviour the row claims to describe (the drift this accessor removes).
//
// It returns a COPY: the returned slice is the caller's, the resolver's list stays
// this package's to own.
func DefaultEffortValues() []string {
	out := make([]string, len(defaultEffortValues))
	copy(out, defaultEffortValues)
	return out
}

// defaultReasoningEffort is the effort used when nothing valid is resolved.
const defaultReasoningEffort = "minimal"

// EffortOff is the harness-only effort value meaning "thinking disabled".
// It is accepted everywhere but never sent on the wire (the caller resolves
// it to thinking disabled).
//
// EXPORTED (Rule 14) because a second package has to answer the same question
// about it: the /config report must know that "off" is the one effort value that
// does NOT imply thinking, so cmd/agent cannot re-type the spelling (Rule 13).
const EffortOff = "off"

// ResolveReasoningEffort is the exported entry point (used by internal/agent
// at effort-resolution time). See resolveReasoningEffort.
func ResolveReasoningEffort(effort string, values []string) (string, bool) {
	return resolveReasoningEffort(effort, values)
}

// resolveReasoningEffort validates a requested reasoning effort against the
// settings.json "effort_values" whitelist and returns the value to actually
// use. Zero vendor mapping lives in Go: the list IS the provider's schema,
// declared as data (like api_key belongs to settings). A vendor that rejects
// an effort anyway is handled at runtime by the minimal-core degradation
// ladder (deepseek error_1214.go rung 1 strips reasoning_effort and re-sends)
// — the error path IS the mapping's runtime implementation.
//
// rules:
//   - "off" always resolves to "off" (harness-only switch, thinking disabled)
//   - nil/empty values list → defaultEffortValues (DeepSeek baseline)
//   - effort in the list (case-insensitive, trimmed match) → effort
//     VERBATIM — the user's casing passes through ("what you send is what
//     goes"; no cross-protocol translation)
//   - anything else → defaultReasoningEffort with ok=false (caller warns)
func resolveReasoningEffort(effort string, values []string) (string, bool) {
	e := strings.ToLower(strings.TrimSpace(effort))
	if e == EffortOff {
		return EffortOff, true
	}
	list := values
	if len(list) == 0 {
		list = defaultEffortValues
	}
	for _, v := range list {
		if strings.ToLower(strings.TrimSpace(v)) == e {
			return effort, true
		}
	}
	return defaultReasoningEffort, false
}

// ValidateReasoningEffortSettings rejects obviously broken effort_values
// lists at settings load time (empty entries, "off" as a member). It returns
// a descriptive error the loader can surface; nil means the list is usable.
func ValidateReasoningEffortSettings(values []string) error {
	for _, v := range values {
		t := strings.TrimSpace(v)
		if t == "" {
			return fmt.Errorf("config: effort_values contains an empty entry")
		}
		if strings.EqualFold(t, EffortOff) {
			return fmt.Errorf("config: effort_values must not contain %q (it is a harness-only switch meaning thinking disabled, never a wire value)", t)
		}
	}
	return nil
}

// shellQuote quotes a token for safe re-injection into a shell command line.
// Paths can carry spaces (a settings file under a spaced directory) — spaces
// are the one character that would break a naive `--settings X task run`
// echo. Single-quote wrapping handles spaces and all shell metacharacters; a
// single quote INSIDE the token is escaped shell-style ('\”), so any path
// round-trips under POSIX sh. Kept dependency-free (no shell quoting lib).
func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if !strings.ContainsAny(s, " \t\n'\"\\$`&;()|<>*?[]{}~") {
		return s // no shell-special characters → verbatim
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// LaunchSettingsArg recovers the settings context THIS process was launched
// under and returns a re-runnable `--settings <PATH>` token-pair (e.g.
// "--settings ~/.gopi/.do_settings.json") when a non-default
// settings file is in effect, or "" when the process runs on the default
// settings path (get the default's /proc/<pid>/cmdline cleanliness — no
// config flag to echo).
//
// Resolution order mirrors main.go's config bootstrapping (GOPI_SETTINGS
// injected as a flag at os.Args level is highest; then the GOPI_SETTINGS env;
// then the default): a caller that typed `gopi --settings X worker T` must
// see X survive into the recorded run provenance, while a run of a
// dispatched/worker child that inherited GOPI_SETTINGS from its parent does
// too. Quoted via shellQuote so a spaced settings path round-trips.
//
// Used by task-run provenance (run_cmd must be the truthful re-runnable
// command: a `--settings X` run recorded as a bare `gopi worker T` would
// silently re-run under a DIFFERENT provider/model when resumed). The
// settings path IS provider identity (api_key/base_url/model live there).
func LaunchSettingsArg() string {
	if p := flagSettingsPath(); p != "" {
		// Explicit --settings wins even if it names the default file (the
		// operator asked for it on the cmdline — the run_cmd records their
		// verbatim launch). Do NOT compare against the default here.
		return "--settings " + shellQuote(p)
	}
	if p := strings.TrimSpace(os.Getenv(SettingsEnv)); p != "" {
		// Env-sourced: GOPI_SETTINGS is commonly baked into a shell profile
		// pointing at the very settings.json that IS the default — echoing
		// `--settings <default>` onto every run_cmd would be noise, not
		// truth. Suppress when the env path equals the default baseline
		// (the default /proc cleanliness the design doc wants).
		if p == DefaultSettingsPath() {
			return ""
		}
		return "--settings " + shellQuote(p)
	}
	return ""
}

// flagSettingsPath scans os.Args for a top-level `--settings <PATH>` or
// `--settings=PATH` flag — the CLI override (highest precedence). Returns ""
// when absent. Does NOT consult the env (env is the caller's second rung).
func flagSettingsPath() string {
	args := os.Args
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--settings" {
			if i+1 < len(args) {
				return args[i+1]
			}
			return ""
		}
		if strings.HasPrefix(a, "--settings=") {
			return strings.TrimPrefix(a, "--settings=")
		}
	}
	return ""
}

// BootstrapEnvFromArgs applies the process-global config-flag overrides to
// the ENVIRONMENT from argv, BEFORE any subcommand dispatch. It is the
// subcommand-side twin of main.go's agent-path flag processing: subcommands
// (gopi task run, gopi worker, gopi memory …) RETURN from main() before the
// top-level --settings/--home config block runs, so a `gopi --settings X task
// run T` launch would otherwise drop X entirely — the child/recovery would
// resolve the default settings and silently change provider. Hoisting the env
// injection ahead of the subcommand chain lets every subcommand (AND its
// spawned child, which inherits os.Environ()) run under the operator's
// explicit configure.
//
// Only flags actually PRESENT on argv are applied (never overrides a caller's
// pre-exported GOPI_SETTINGS/GOPI_HOME absent a flag). Idempotent for the
// agent path: the later config block re-reads the same env vars and finds
// them already set to the flag's value (settings fill = no change).
//
// Recognized: `--settings PATH|--settings=PATH`, `--home PATH|--home=PATH`.
// Unknown leading flags are left untouched (they may be subcommand flags or
// ssh-pass-through — not ours to consume here).
//
// The RETURN value is argv with each recognized flag (and its paired value)
// REMOVED — the token-slice the caller should dispatch subcommands on. This
// is the other half of the bootstrap contract: the env hoist alone fixes the
// child's settings resolution, but the subcommand chain still matched the RAW
// argv. `gopi --settings X task submit "a b c"` reached isTasksSubcommand
// with args[0]=="--settings"; its ssh-passthrough branch then misread the
// settings PATH as the ssh HOST and word-split the remainder
// (strings.Fields over strings.Join), shredding the quoted payload into
// positionals ("a","b","c") — the "unexpected positional argument" bug. A
// caller that dispatches on the returned remainder sees `task submit "a b c"`
// with the quoting intact (positional-param bug fix, 2026-09-06).
func BootstrapEnvFromArgs(args []string) []string {
	envVarFor := map[string]func(string){
		"settings": func(p string) { os.Setenv(SettingsEnv, p) },
		"home":     func(p string) { os.Setenv(GopiDirEnv, p) },
		// --tmpdir is hoisted for the same reason --settings/--home are: the
		// group verbs (`task worker`, `task run`, `merger`) return from main()
		// at the subcommand chain and never reach the agent-path config block,
		// so a `gopi --tmpdir X worker T` launch would otherwise drop X and
		// the child tree would fall back to the default. It writes the KNOB's env
		// name (GOPI_TMPDIR), not TMPDIR: the publisher converts the knob into
		// TMPDIR after ranking the channels, and publishing TMPDIR here would
		// make the flag indistinguishable from an ambient override.
		TmpDirFlagName: func(p string) { os.Setenv(TmpDirEnvName, p) },
		// --scratch-gc-stale-min is hoisted for the same reason: `gopi
		// --scratch-gc-stale-min 90 merger` returns from main() at the
		// subcommand chain, before the agent-path config block, so without the
		// hoist the resident sweep would run with the DEFAULT window and the
		// operator's value would be silently dropped. It writes the knob's env
		// name (scratch.GCStaleAgeEnvName) — the ONE representation the
		// resolution site reads (Rule 2/13).
		ScratchGCStaleMinFlagName: func(p string) { os.Setenv(scratch.GCStaleAgeEnvName, p) },
		// --output-language is hoisted for a stronger version of the same
		// reason: `gopi chat` — the surface the operator was looking at when he
		// asked for this knob — returns from main() at the subcommand chain and
		// NEVER reaches the agent-path config block, so a flag parsed only
		// there would be silently dropped by the very verb it exists for. It
		// writes the knob's env name (OutputLanguageEnv) — the ONE
		// representation the resolution reads (Rule 2/13). The write is
		// unconditional, which is exactly how a flag outranks an operator's
		// export: `gopi --output-language en-GB chat` replies in en-GB even
		// with GOPI_OUTPUT_LANGUAGE=zh-CN exported.
		OutputLanguageFlagName: func(p string) { os.Setenv(OutputLanguageEnv, p) },
		// --memory-prefetch is hoisted for exactly the reason above: this knob
		// exists FOR `gopi chat`, which returns from main() at the subcommand chain
		// and never reaches the agent-path config block, so a flag parsed only
		// there would be silently dropped by the only verb it serves. The write is
		// unconditional, which is how a flag outranks an operator's export:
		// `gopi --memory-prefetch=off chat` prefetches nothing even with
		// GOPI_MEMORY_PREFETCH=1 exported.
		MemoryPrefetchFlagName: func(p string) { os.Setenv(MemoryPrefetchEnv, p) },
		// --chat-ideal-geometry is hoisted for exactly the reason above: the
		// knob exists FOR `gopi chat` (and for the replay/beat filmer that
		// draws the same surface), and `gopi chat` returns from main() at the
		// subcommand chain without ever reaching the agent-path config block. It
		// writes the knob's env name through the knob's OWN publisher, so the
		// flag has exactly one spelling and one writer (Rule 13/14).
		ChatIdealGeometryFlagName: PublishChatIdealGeometryFlag,
		// --chat-surface-turns is hoisted for exactly the reason above: the knob
		// exists FOR `gopi chat`, which returns from main() at the subcommand
		// chain without ever reaching the agent-path config block, so a flag
		// parsed only there would be silently dropped by the very verb it
		// serves. It writes the knob's env name through the knob's OWN
		// publisher, so the flag has one spelling and one writer (Rule 2/13/14).
		ChatSurfaceTurnsFlagName: PublishChatSurfaceTurnsFlag,
		// --film-terminal is hoisted for the same reason one line up: the knob
		// exists FOR `gopi chat --film`, which returns from main() at the
		// subcommand chain and never reaches the agent-path config block, so a
		// flag parsed only there would be silently dropped by the very verb it
		// serves. It writes the knob's env name through the knob's OWN publisher
		// (Rule 2/13/14 — one spelling, one writer).
		FilmTerminalFlagName: PublishFilmTerminalFlag,
	}
	out := make([]string, 0, len(args))
	// seenVerb latches on the first non-flag word: from there on a flag is the
	// SUBCOMMAND's business, and a top-level hoist that kept consuming would eat
	// a verb or a value the subcommand owns. Together with the conditional peek
	// below (prefetchBoundEnv) it is what makes
	// `gopi --memory-prefetch chat` mean "arm the knob, then run chat" instead of
	// "arm the knob with the word chat and run nothing".
	seenVerb := false
	verbBound := prefetchBoundEnv()
	for i := 0; i < len(args); i++ {
		a := args[i]
		name := ""
		val := ""
		eq := false
		if strings.HasPrefix(a, "--") {
			body := strings.TrimPrefix(a, "--")
			if k := strings.IndexByte(body, '='); k >= 0 {
				name, val, eq = body[:k], body[k+1:], true
			} else {
				name, val = body, ""
			}
		}
		if fn, ok := envVarFor[name]; ok {
			if seenVerb && verbBound[name] {
				out = append(out, a) // after the verb the token is the SUBCOMMAND's business
				continue
			}
			if eq {
				// THE INLINE FORM IS A DECLARATION TOO, so a value-less or
				// unusable `--flag=` is refused exactly like the spaced form
				// (see hoistedFlagVerdict): `--chat-surface-turns=` used to
				// publish nothing and leave the run at the built-in bound while
				// the operator believed they had set one (Rule 15).
				if reason := hoistedFlagVerdict(name, val, true); reason != hoistOK {
					refuseHoistedFlagValue(name, val, reason)
				}
			}
			if !eq {
				// `--name value`: peek the next arg for the value.
				//
				// THE PEEK IS NOT UNCONDITIONAL FOR A SHAPE-CHECKED FLAG. It
				// used to be, and that is the defect this branch closes: the
				// token after the flag was taken as its value whatever it was,
				// so `gopi --chat-surface-turns chat` (a value-less flag in
				// front of its own verb) published the WORD "chat" into the
				// knob's env and DELETED it from the dispatch remainder — the
				// verb vanished, no subcommand was dispatched, and the process
				// fell through to the bare-prompt path printing the top-level
				// usage with exit 0. The command the operator typed had been
				// silently rewritten into a different one.
				//
				// The verdict below asks the knob's OWN parser whether the
				// token is the value it needs (Rules 2/15), and a NO refuses the
				// process by name instead of consuming it. It is asked before the
				// peek so the token is never consumed on the way to the refusal,
				// and it returns refuse=false for every flag whose value is a
				// path or a free string — for those, "the next token is the
				// value" is the only answer available and a guess would be worse
				// than the convention.
				{
					next := ""
					haveToken := i+1 < len(args)
					if haveToken {
						next = args[i+1]
					}
					if reason := hoistedFlagVerdict(name, next, haveToken); reason != hoistOK {
						refuseHoistedFlagValue(name, next, reason)
					}
				}
				if i+1 < len(args) {
					if verbBound[name] {
						// A verb-bound flag's value is OPTIONAL, so the peek must
						// be CONDITIONAL: it takes the next token only when that
						// token is a boolean spelling. `gopi --memory-prefetch chat`
						// therefore arms the knob and leaves `chat` in argv, instead
						// of aiming the switch at the verb and deleting it.
						if _, isBool := normalizedBoolValue(args[i+1]); isBool {
							val = args[i+1]
							i++
						}
					} else {
						val = args[i+1]
						i++
					}
				} else {
					if !verbBound[name] {
						continue // dangling flag, no value — nothing to set
					}
				}
				if val == "" && verbBound[name] {
					val = EnvBoolOn // bare verb-bound flag means ON
				}
			}
			if val != "" {
				fn(val)
			}
			continue // the flag token (and pair value) are not copied to out
		}
		out = append(out, a)
		if !strings.HasPrefix(a, "--") {
			seenVerb = true
		}
	}
	// Nothing consumed → return the input unchanged (identity semantics: the
	// plain `gopi task …` path must see the same slice it always did).
	if len(out) == len(args) {
		return args
	}
	return out
}
