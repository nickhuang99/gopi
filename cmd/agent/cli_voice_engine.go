package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/doubaoasr"
)

// The voice_input ENGINE SEAM.
//
// `voice_input` has had exactly ONE transcription core since it was written:
// transcribeFile(opts, audioPath) — the function the coder comment calls "the
// single core both server shapes call" (cli_voice.go). Every shape that
// produces text funnels through it:
//
//	one-shot    gopi tool voice_input <audio>            cli_voice.go
//	HTTP server gopi tool voice_input --server           cli_voice_server.go
//	pipe server gopi tool voice_input --server --stdio   cli_voice_server.go
//
// and the capture client (`--client`, `--client --stdio`) stays engine-BLIND
// on purpose: it records PCM and hands it to a transcriber, so which engine
// runs is decided on the far side of the hop, not by the machine holding the
// microphone. That is why the dispatch lives INSIDE transcribeFile instead of
// at each call site — one seam, one place to read, no shape able to drift.
//
// THE DEFAULT IS `doubao` (Volcengine cloud), flipped 2026-10-07 on the
// operator's instruction. The argument is devdoc/doubao-asr-integration.md §3;
// its short form is that the two engines carry UNEQUAL BURDENS, so the default
// belongs to the lighter one:
//
//  1. What the machine must ALREADY HAVE. `local` needs 1.7 GB of weights on
//     disk (910 MB loaded per run) plus the llama.cpp runtime binary; `doubao`
//     needs 0 B of either. A default is what an operator gets on a machine
//     where nothing was deployed — that is the cloud engine — so the engine
//     that needs an installation is the one reached BY NAME (--engine local /
//     $GOPI_VOICE_ENGINE=local / settings "voice_engine": "local").
//  2. The FIRST FAILURE of each default, weighed honestly. Under `local` it was
//     "the weights are not here" — a 1.7 GB deployment the operator had to
//     discover. Under `doubao` it is "the credential is not here" — one line to
//     fix, and refused BEFORE the microphone is opened (voiceDepsReady), with
//     the missing knob named. Neither is free; the correction is not equal.
//
// The cost of the flip is PRIVACY, and it is paid in the open rather than
// hidden: every run that resolves to this engine prints
// voiceDoubaoPrivacyNotice (below) at the moment it commits, saying that the
// audio LEAVES this machine, which endpoint receives it, and the three channels
// that reach `local`. Silence of the operator now means "upload" — a decision
// worth stating every single time rather than leaving to be inferred. And the
// refusal half is unchanged in KIND: an engine name that is not one of the two
// is still refused, never silently downgraded (voiceValidateEngine).

// Voice engine labels reported by the HTTP contract (GET /health and the
// /transcribe response). "funasr-nano" is the label those endpoints have
// printed for the local engine since they were written — kept VERBATIM, so an
// existing client's parser and the contract tests keep reading the same
// string; the cloud engine gets its own label rather than reusing the local
// one, because a transcript that came from the vendor's servers should say so
// in the response (a client that must not send audio off-machine can check).
const (
	voiceEngineLabelLocal  = "funasr-nano"
	voiceEngineLabelDoubao = "doubao"
	// voiceEngineLabelDoubaoStream is the streaming arm's label. It has its own
	// value rather than sharing "doubao" because the two are different channels
	// with different latency and different billing — a client that must know
	// whether its audio was uploaded in real time is entitled to tell them
	// apart from the response alone.
	voiceEngineLabelDoubaoStream = voiceStreamEngineLabel
)

// voiceEngineLabel is the engine name the HTTP contract reports for a run.
func voiceEngineLabel(opts *voiceOptions) string {
	switch voiceResolveEngine(opts) {
	case config.AudioEngineRecording:
		return voiceEngineLabelDoubao
	case config.AudioEngineStreaming:
		return voiceEngineLabelDoubaoStream
	case config.AudioEngineSauc:
		return voiceSaucEngineLabel
	}
	return voiceEngineLabelLocal
}

// Doubao engine error prefixes (named: the CLI, the tests and the devdoc all
// quote them, so they are one spelling — Rule 1b).
const (
	voiceDoubaoKeyHint    = "doubao engine needs an API key"
	voiceDoubaoKeyFileEnv = config.AudioKeyFileEnv
)

// voiceBackToLocalHint is the ONE sentence that tells an operator how to get
// back to on-machine transcription. Two surfaces owe it — the privacy notice
// (a run that IS uploading) and the missing-key refusal (a run that cannot) —
// so it is written once here rather than twice at the call sites, where the
// channels could drift apart (Rule 1b/13).
//
// All three channels are named because they are the three the resolver reads:
// a caller who cannot pass a flag still has an env var and a settings key.
func voiceBackToLocalHint() string {
	return fmt.Sprintf("to transcribe ON this machine instead: --engine %s, %s=%s, or \"%s\": %q in %s",
		config.AudioEngineLocal, config.AudioEngineEnv, config.AudioEngineLocal,
		config.AudioEngineSettingsKey, config.AudioEngineLocal, config.ResolveSettingsPath())
}

// voiceDoubaoPrivacyNotice is the line every run that is ABOUT to upload says,
// and it exists because the default flipped: the cloud engine is no longer
// something an operator had to ask for, so "audio leaves this machine" is a
// fact they must be told rather than a choice they remember making.
//
// It states the four things the decision needs: WHAT is sent (the audio), WHERE
// it goes (the endpoint that receives it, and the resource id it is billed
// against), WHETHER anything chose it (an explicit name reads differently from
// the default — see voiceResolveEngine), and HOW to take it back (the three
// channels, from voiceBackToLocalHint). It carries no key, no path to a key and
// no audio: an endpoint and a resource id are not secrets.
//
// It is an ANNOUNCEMENT, not a gate (Rule 35: no new persistent state, no
// first-run confirmation record, nothing to clean up). A refusal here would be
// a second, invisible default; the operator's lever is the flag they already
// have.
func voiceDoubaoPrivacyNotice(opts *voiceOptions) string {
	who := "the default — no --engine, no $" + config.AudioEngineEnv +
		" and no \"" + config.AudioEngineSettingsKey + "\" settings key chose an engine"
	if voiceExplicitlyChoseDoubao(opts) {
		who = "chosen explicitly on one of the three channels (flag, env or settings)"
	}
	return fmt.Sprintf(
		"[voice] PRIVACY — engine %s UPLOADS the audio to %s (X-Api-Resource-Id %s); the recording leaves this machine (%s). %s",
		config.AudioEngineRecording,
		voiceDoubaoEffectiveSubmitURL(opts),
		voiceDoubaoSettingSource(opts).resourceID,
		who,
		voiceBackToLocalHint())
}

// voiceReportDoubaoPrivacy writes the notice above to stderr. Every caller that
// is about to hand audio to the cloud engine calls it BEFORE the upload, so the
// announcement is never something the operator reconstructs from a transcript
// that already came back.
func voiceReportDoubaoPrivacy(opts *voiceOptions) {
	fmt.Fprintf(os.Stderr, "%s\n", voiceDoubaoPrivacyNotice(opts))
}

// voiceEngineValues are the accepted --engine / GOPI_VOICE_ENGINE / settings
// values, in the order the help lists them.
var voiceEngineValues = []string{config.AudioEngineLocal, config.AudioEngineRecording, config.AudioEngineStreaming, config.AudioEngineSauc}

// voiceResolveEngine resolves the engine selection from the three rungs the
// whole feature uses — explicit flag > env > settings — with
// config.AudioEngineDefault (the CLOUD engine, since 2026-10-07) as the
// terminal default. "" means "nobody chose".
//
// The flag rung is already folded into opts.engine by parseVoiceArgs (it is
// the one rung the parser owns); this function adds the other two. It is
// called from several places (dispatch, the HTTP label, the deps check, the
// chat mode's entry), so it must stay free of side effects: a resolver that
// read the key file or touched the network would make the /health label a
// network call.
//
// A CALLER THAT ANNOUNCES MUST GUARD ON THIS DEFAULT, and the reason is the
// flip itself. Before it, "the engine is doubao" was evidence the operator had
// ASKED for the cloud; now it is what they get by saying nothing, so a sentence
// whose meaning is "you chose to send audio off the machine" is only true when
// the selection is EXPLICIT — Compare opts.engine (what the parser recorded)
// against the env and the settings, or call voiceExplicitlyChoseDoubao. The
// privacy notice (voiceDoubaoPrivacyNotice) is the caller that must not get
// this wrong: telling an operator they chose something they never typed is its
// own defect, distinct from the upload itself.
func voiceResolveEngine(opts *voiceOptions) string {
	if v := strings.TrimSpace(opts.engine); v != "" {
		return audioEngineName(v)
	}
	// The env rung reads the CURRENT variable and, when it is unset, the
	// PRE-RENAME one — at the SAME rung, because a script that exports
	// GOPI_VOICE_ENGINE is no less explicit than one that exports
	// GOPI_AUDIO_ENGINE. Which one answered is reported once per run by
	// voiceReportLegacyAudioConfig.
	if v := strings.TrimSpace(os.Getenv(config.AudioEngineEnv)); v != "" {
		return audioEngineName(v)
	}
	if v := strings.TrimSpace(os.Getenv(config.AudioEngineLegacyEnv)); v != "" {
		return audioEngineName(v)
	}
	cfg, err := config.FromSettingsFile(config.ResolveSettingsPath())
	if err != nil || cfg == nil {
		return config.AudioEngineDefault
	}
	// cfg.AudioEngine already carries the settings rung AND the pre-rename
	// translation (the load path folds "voice_engine" in and maps the old
	// engine names — normalizeAudioParams), so no second translation is needed
	// here; the call below is idempotent for a name that is already current.
	if v := strings.TrimSpace(cfg.AudioEngine); v != "" {
		return audioEngineName(v)
	}
	return config.AudioEngineDefault
}

// audioEngineName maps a PRE-RENAME engine name onto the functional name it
// means today ("doubao" → "recording", "doubao-stream" → "streaming") and
// returns anything else unchanged. The mapping itself lives in internal/config
// (AudioEngineLegacyVendorName) so the resolver, the settings view and the
// tests cannot hold three opinions about what "doubao" names (Rule 2).
func audioEngineName(name string) string {
	translated, _ := config.AudioEngineLegacyVendorName(name)
	return translated
}

// voiceExplicitlyChoseDoubao reports whether SOME channel named the cloud
// engine — as opposed to a channel naming nothing and the default answering
// "doubao". It is the ONE predicate behind the difference between "you asked
// for the cloud" and "the cloud is what you get for saying nothing" (see
// voiceResolveEngine's note), so the two messages cannot drift.
//
// It reads the same three knobs through the same precedence (Rule 2) rather
// than a second copy of the chain: the flag rung, the env rung, the settings
// rung — the first rung that names ANY engine decides, and only an explicit
// cloud value counts as a choice.
func voiceExplicitlyChoseDoubao(opts *voiceOptions) bool {
	if v := strings.TrimSpace(opts.engine); v != "" {
		return audioEngineName(v) == config.AudioEngineRecording
	}
	if v := strings.TrimSpace(os.Getenv(config.AudioEngineEnv)); v != "" {
		return audioEngineName(v) == config.AudioEngineRecording
	}
	if v := strings.TrimSpace(os.Getenv(config.AudioEngineLegacyEnv)); v != "" {
		return audioEngineName(v) == config.AudioEngineRecording
	}
	cfg, err := config.FromSettingsFile(config.ResolveSettingsPath())
	if err != nil || cfg == nil {
		return false
	}
	return audioEngineName(strings.TrimSpace(cfg.AudioEngine)) == config.AudioEngineRecording
}

// voiceValidateEngine rejects an unknown engine name LOUDLY, naming the two
// accepted values. Failing here (at parse time, before any work) is what keeps
// a typo from silently falling back to the local engine — a silent fallback
// would send the wrong audio somewhere or, worse, look like the cloud ran.
func voiceValidateEngine(name string) error {
	if name == "" {
		return nil
	}
	// A PRE-RENAME name is accepted, and the acceptance is a translation rather
	// than a second entry in the list: the operator's script says "doubao", the
	// engine that runs is `recording`, and voiceReportLegacyAudioConfig says so
	// out loud. Accepting it here is what keeps the migration from being a
	// breaking change; translating it (rather than matching it in the list) is
	// what keeps ONE name per engine in the surfaces that report it.
	if translated, wasLegacy := config.AudioEngineLegacyVendorName(name); wasLegacy {
		name = translated
	}
	for _, v := range voiceEngineValues {
		if name == v {
			return nil
		}
	}
	return fmt.Errorf("unknown engine %q — want one of %s (default %s)",
		name, strings.Join(voiceEngineValues, ", "), config.AudioEngineDefault)
}

// voiceResolveDoubaoOpts assembles the cloud engine's request from the three
// rungs (flag/env/settings) exactly like the engine selection itself. It
// returns a fully-populated doubaoasr.Options with the resource_id EXPLICIT
// (never left to a client-side default) and the key loaded from the location
// voiceDoubaoKeySource chose — an explicitly named key FILE, the inline
// settings value, or the legacy ~/.doubao/apikey.txt fallback. The key value
// itself is never printed, never logged and never written to a report; the
// ONLY thing that happens to it below is a transfer into the request's header.
func voiceResolveDoubaoOpts(opts *voiceOptions) (doubaoasr.Options, error) {
	settings := voiceDoubaoSettingSource(opts)

	src := voiceDoubaoKeySource(opts)
	voiceReportKeyMode(src)
	key, err := src.load()
	if err != nil {
		return doubaoasr.Options{}, fmt.Errorf("%s (%s): %w", voiceDoubaoKeyHint, src.hint(), err)
	}

	do := doubaoasr.Options{
		ResourceID: settings.resourceID,
		APIKey:     key,
		AudioFile:  opts.audio,
		// THE ENDPOINTS, resolved from the ONE root: an individual URL named
		// on an explicit channel is more specific than the root and wins for
		// itself (opts.doubaoSubmit/doubaoQuery come from the CLI flags, which
		// are the most specific channel there is); otherwise BOTH derive from
		// the resolved root, so a regional base can never send the submit to
		// one host and poll the other's task id.
		SubmitURL:      stringOrDefault(opts.doubaoSubmit, doubaoasr.SubmitURLFor(settings.baseURL)),
		QueryURL:       stringOrDefault(opts.doubaoQuery, doubaoasr.QueryURLFor(settings.baseURL)),
		Hotwords:       doubaoasr.SplitHotwords(settings.hotwords),
		EnablePUNC:     true,
		EnableITN:      true,
		ShowUtterances: true,
	}
	return do, nil
}

// voiceDoubaoEffectiveSubmitURL is the URL a batch run will actually POST to —
// what the privacy notice announces, computed by the SAME precedence the
// request assembly uses (Rule 2: one resolution, so the announcement and the
// wire cannot disagree about where the audio went). It is a function rather
// than a field so the notice needs no second copy of the chain.
func voiceDoubaoEffectiveSubmitURL(opts *voiceOptions) string {
	if v := strings.TrimSpace(opts.doubaoSubmit); v != "" {
		return v
	}
	return doubaoasr.SubmitURLFor(voiceDoubaoSettingSource(opts).baseURL)
}

// doubaoKeySource is WHERE the doubao credential was found, and WHAT to read
// once that is settled. It is the ONE resolution point for the key (Rule 2):
// the deps check, the request assembly and every message quote it, so no
// second reading of the same knobs can disagree about which rung won.
//
// THE PRECEDENCE, spelled once — and the one asymmetry that makes it a
// decision rather than an accident:
//
//	--doubao-key-file  >  $GOPI_DOUBAO_KEY_FILE  >  settings "doubao_key_file"
//	                   >  $GOPI_DOUBAO_API_KEY    >  settings "doubao_api_key"
//	                   >  ~/.doubao/apikey.txt (legacy fallback)
//
// A FILE named on an explicit channel outranks the inline value because a
// stored path is a deliberate statement about where the credential lives ("the
// key is on this host's encrypted volume, not in a file gopi reads for
// everything else"), and overwriting that statement with whatever happens to
// sit in settings.json would be gopi second-guessing the operator. The inline
// key is what fills the gap when nobody named a file — which is exactly the
// operator's own framing: the vision credential already lives in settings.json,
// so the voice one may too.
type doubaoKeySource struct {
	// file is the key-file path an explicit channel chose ("" when none did:
	// the legacy default is applied at load time, so "nobody chose" stays
	// distinguishable from "chose the default" in every message).
	file string
	// inline is the key VALUE settings.json or its env channel carries (""
	// when neither does). NEVER printed, never logged.
	inline string
	// inlineFrom names the channel the inline value came from ("settings
	// "doubao_api_key"" / "$GOPI_DOUBAO_API_KEY"), so a refusal can point at the
	// thing the operator actually set.
	inlineFrom string
	// settingsPath is the document both were read from — the file whose mode
	// the credential guard tightens when it carries a secret.
	settingsPath string
	// modeNote is the sentence to print when the guard HAD to tighten the
	// document's mode, and modeProblem the sentence to print when it could not.
	// They are fields rather than a direct write to stderr so the caller (and
	// the tests) can see the decision: a permission change on the operator's
	// own file that nobody was told about is the defect, and a print buried
	// inside a resolver cannot be asserted.
	modeNote    string
	modeProblem string
	// legacyFile is the terminal fallback path, resolved from $HOME.
	legacyFile string
}

// voiceDoubaoKeySource resolves the credential's location. It DOES NOT read the
// value out of a file (that is load()) so the deps check can answer "is a key
// available?" without touching the secret.
//
// Side effect, stated because it is one: when the settings document carries the
// inline key, its file mode is tightened to 0600 FIRST and the change is
// announced on stderr. Writing or reading a credential out of a world-readable
// document would be the security downgrade the legacy location is being
// replaced to avoid (measured on this host: the live settings.json is 0664).
func voiceDoubaoKeySource(opts *voiceOptions) doubaoKeySource {
	src := doubaoKeySource{
		settingsPath: config.ResolveSettingsPath(),
		legacyFile:   doubaoasr.DefaultAPIKeyPath(userHomeDirOrDefault()),
	}
	if cfg, err := config.FromSettingsFile(src.settingsPath); err == nil && cfg != nil {
		// THE SETTINGS AND ENV RUNGS COME FROM ONE READER (Rule 2):
		// config.ResolveAudioRecording applies env > nested block, with the
		// pre-rename top-level keys already folded into the block by the load
		// path. The CLI's flag rung is applied at the bottom of this function,
		// so the whole precedence is spelled twice at most and the two cannot
		// disagree about which channel won.
		//
		// The stored PATH keeps its own rung: it is a named location, and it
		// outranks the inline value.
		rec := config.ResolveAudioRecording(cfg)
		src.file = strings.TrimSpace(rec.KeyFile)
		if v := strings.TrimSpace(rec.APIKey); v != "" {
			src.inline, src.inlineFrom = v, fmt.Sprintf("settings %q",
				config.AudioParamSettingsPath(config.AudioEngineRecording, config.AudioParamKeyAPIKey))
		}
		// The mode guard runs whether or not THIS call ends up using the
		// inline value: the document carries a credential either way, and the
		// moment gopi reads the file is the moment it can protect it.
		if note, cerr := config.EnsureSettingsPrivateForCredential(src.settingsPath); cerr != nil {
			// A mode that cannot be tightened is REPORTED, never swallowed
			// (Rule 15): the operator is about to have a credential handled out
			// of a file gopi could not protect, and that must not be a silence.
			src.modeProblem = cerr.Error()
		} else {
			src.modeNote = note
		}
	}
	// The credential's ENV channels were already applied by
	// config.ResolveAudioRecording above (the CURRENT name and the pre-rename
	// one), so nothing is read twice here (Rule 2) and the env rung stays above
	// the settings rung by construction.
	// The FLAG is the last writer, so it wins (the same overwrite-in-order
	// shape voiceDoubaoSettingSource uses — one precedence, no per-rung
	// "already set?" branches to drift).
	if v := strings.TrimSpace(opts.doubaoKeyFile); v != "" {
		src.file = v
	}
	return src
}

// voiceReportKeyMode announces a settings-permission decision on stderr. Both
// sentences name the PATH and the MODES and neither can carry the key value
// (they are written before the value is read, and they quote only the path) —
// the report's whole job is to say "your credential file was world-readable and
// is not any more", which an operator must learn at the moment it happens.
func voiceReportKeyMode(src doubaoKeySource) {
	if src.modeProblem != "" {
		fmt.Fprintf(os.Stderr, "[gopi] %s\n", src.modeProblem)
		return
	}
	if src.modeNote != "" {
		fmt.Fprintf(os.Stderr, "[gopi] %s\n", src.modeNote)
	}
}

// missing reports the first reason no key can be produced, or "" when one can.
// It is the deps check's whole body — one predicate, so "the key is available"
// cannot mean one thing to the pre-flight check and another to the request.
//
// The INLINE rung has nothing to stat: the credential is in the settings
// document itself (whose mode the key source has already tightened and
// reported). It also never prints: this function's entire output is a message
// an operator reads, so the ONLY thing it may quote is a path or a channel name.
func (s doubaoKeySource) missing() string {
	if s.inline != "" {
		return ""
	}
	path := s.file
	if path == "" {
		path = s.legacyFile
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Sprintf("%s: %s (%s). %s", voiceDoubaoKeyHint, path, s.hint(), voiceBackToLocalHint())
	}
	return ""
}

// load returns the key the source resolves to, or the error that says WHY no
// key could be produced — naming the rung that was tried, never the value.
func (s doubaoKeySource) load() (string, error) {
	if s.file != "" {
		return doubaoasr.LoadAPIKey(s.file)
	}
	if s.inline != "" {
		return s.inline, nil
	}
	// The legacy fallback: ~/.doubao/apikey.txt. Kept working on purpose — an
	// existing deployment must not break because a better location was added.
	return doubaoasr.LoadAPIKey(s.legacyFile)
}

// hint names the knob that decides WHERE the key comes from, so a missing key
// points at a setting the operator can change rather than at the engine.
func (s doubaoKeySource) hint() string {
	if s.file != "" {
		return voiceDoubaoKeyFileHint(s.file)
	}
	if s.inlineFrom != "" {
		return fmt.Sprintf("inline key from %s", s.inlineFrom)
	}
	return voiceDoubaoKeyFileHint(s.legacyFile)
}

// voiceDoubaoKeyFileHint names the knob that chose the key file, so a missing
// file points at the setting the operator can change.
func voiceDoubaoKeyFileHint(path string) string {
	return fmt.Sprintf("key file %q (set the engine's key file with %s or settings %q)",
		path, voiceDoubaoKeyFileEnv,
		config.AudioParamSettingsPath(config.AudioEngineRecording, config.AudioParamKeyKeyFile))
}

// doubaoSettingSource is the resolved cloud configuration that is NOT the
// credential: what the flag, the env and the settings file agreed on. The key
// has its own resolver (voiceDoubaoKeySource) because it is the one value that
// is a SECRET — redaction, permission and precedence rules differ for it, and
// folding it back in here is how a credential ends up printed by a function
// that prints configuration.
type doubaoSettingSource struct {
	resourceID     string
	hotwords       string
	baseURL        string
	streamEndpoint string
}

// voiceDoubaoSettingSource resolves the cloud knobs in precedence order
// flag > env > settings > named default. Reading the settings file happens
// HERE (and in voiceResolveEngine) and nowhere else for these knobs (Rule 2).
//
// The order the rungs are applied is therefore settings → env → flag: each
// later write overwrites the previous one, so the FLAG — written last — is the
// one that wins. (A "read the flag first, then skip if set" shape encodes the
// same precedence as three more branches; overwriting is the version that
// cannot drift rung by rung.)
//
// The ENDPOINTS follow the same shape and no new mechanism: baseURL is the
// batch channel's root (the setting --doubao-submit-url / --doubao-query-url
// remain more specific than, because each names ONE url and this names the
// root they derive from), and streamEndpoint is the streaming channel's whole
// URL. Both default to "" here rather than to a pre-baked URL string, and that
// is deliberate: the terminal default belongs to the package that OWNS the
// endpoint (doubaoasr.DefaultBaseURL / doubaostream.DefaultEndpoint), applied
// by the consumer through that package's own accessor, so this resolver cannot
// become a second source of the published value (Rule 13).
func voiceDoubaoSettingSource(opts *voiceOptions) doubaoSettingSource {
	src := doubaoSettingSource{
		resourceID: doubaoasr.ResourceSeedASRAUC20,
	}
	cfg, err := config.FromSettingsFile(config.ResolveSettingsPath())
	if err == nil && cfg != nil {
		// ONE reader per block (Rule 2): the nested settings block with the
		// environment applied over it, the pre-rename top-level keys already
		// folded in by the load path. The FLAG rung is applied below, last.
		rec := config.ResolveAudioRecording(cfg)
		src.resourceID = doubaoasr.ResourceIDOrDefault(rec.ResourceID)
		src.hotwords = rec.Hotwords
		src.baseURL = strings.TrimSpace(rec.APIBaseURL)
		src.streamEndpoint = strings.TrimSpace(config.ResolveAudioStreaming(cfg).Endpoint)
	}
	src.resourceID = doubaoasr.ResourceIDOrDefault(stringOrDefault(opts.doubaoResource, src.resourceID))
	src.hotwords = stringOrDefault(opts.doubaoHotwords, src.hotwords)
	src.baseURL = stringOrDefault(opts.doubaoBaseURL, src.baseURL)
	src.streamEndpoint = stringOrDefault(opts.doubaoStreamEndpoint, src.streamEndpoint)
	return src
}

// stringOrDefault returns v unless it is blank (then def).
func stringOrDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return strings.TrimSpace(v)
}

// voiceDoubaoDepsReady reports the first missing dependency of the cloud arm,
// or "" when the run can proceed. The engine needs exactly two things and no
// models: a readable key file (the credential) and a resource_id (which has a
// named default). Both are reported BEFORE the first network call so a
// one-second fix never costs a round trip — the same fail-fast contract the
// local arm's model check provides.
func voiceDoubaoDepsReady(opts *voiceOptions) string {
	src := voiceDoubaoKeySource(opts)
	// The REPORT belongs here as much as in the resolver, and this was
	// MEASURED rather than reasoned: the pre-flight check runs FIRST on the CLI
	// path, so it is the call that tightens a world-readable settings document —
	// and a version that reported only from the resolver announced nothing at
	// all (observed: mode 0664 → 0600 with an empty stderr, 2026-10-07). The two
	// sites cannot double-print: whichever runs second finds the file already
	// private and produces no note.
	voiceReportKeyMode(src)
	return src.missing()
}

// transcribeFileDoubao runs the cloud engine over one audio file — the second
// arm of the engine dispatch, same signature and same error discipline as the
// local arm, so every caller stays engine-blind.
//
// The file is transcribed by 录音文件识别 (volc.seedasr.auc): submit the audio,
// poll until the transcript is ready. It is the batch shape, deliberately — an
// earlier real probe established that this account is NOT granted the streaming
// resource (volc.seedasr.sauc.duration, HTTP 403), so the working channel is
// the one implemented (devdoc/doubao-asr-integration.md §流式探测).
func transcribeFileDoubao(opts *voiceOptions, audioPath string) (string, time.Duration, error) {
	o := *opts
	o.audio = audioPath // the caller's path is the authority (a spooled upload, an ssh hop)
	do, err := voiceResolveDoubaoOpts(&o)
	if err != nil {
		return "", 0, err
	}
	start := time.Now()
	res, err := doubaoasr.Transcribe(context.Background(), do)
	if err != nil {
		// "NO VALID SPEECH" IS NOT A FAILURE — it is the engine's SILENCE
		// verdict, and the two must not be conflated (task 01a11b39).
		//
		// WHAT IT COST TO LEARN THIS. The service answers a window that carried
		// no speech with a COMPLETED job whose transcript is empty (measured on
		// this account 2026-10-08, logid 2026100819203030ED41BEECA127FF8C6E:
		// 20000003, body {"audio_info":{"duration":6000},"result":{"text":""}} —
		// the duration proves the window was received). The voice mode's caller
		// treats a non-nil error as "a round that FAILED" and LEAVES the mode,
		// so one quiet window — a thinking pause, a breath, the tail of a window
		// that opened before speech — switched the microphone off for the rest of
		// the session. That is the "54 seconds of nothing, then a refusal"
		// incident: the mode had already exited on its FIRST round, and every
		// later window the operator spoke into belonged to a mode that was off.
		//
		// The verdict is returned as an EMPTY TRANSCRIPT, which is exactly what
		// it is. The callers that DO treat silence specially (the voice mode's
		// silence arm, `chatVoiceSilentNote`) then work as designed; a caller
		// that ignores it appends nothing, which is also correct. Every OTHER
		// error — a 403, a quota refusal, an unreachable host — still fails.
		var apiErr *doubaoasr.APIError
		if errors.As(err, &apiErr) && apiErr.NoSpeech() {
			return "", time.Since(start), nil
		}
		return "", 0, voiceDoubaoError(err)
	}
	elapsed := time.Since(start)
	return res.Text, elapsed, nil
}

// voiceDoubaoError turns a doubaoasr failure into the operator-facing line,
// keeping the vendor's own verdict (HTTP status, X-Api-Status-Code, log id) in
// the message. Rule 15: the raw code is what the vendor's support asks for, so
// swallowing it would make every incident start from zero.
func voiceDoubaoError(err error) error {
	var apiErr *doubaoasr.APIError
	if errors.As(err, &apiErr) {
		return fmt.Errorf("doubao engine: %s", apiErr.Error())
	}
	return fmt.Errorf("doubao engine: %w", err)
}
