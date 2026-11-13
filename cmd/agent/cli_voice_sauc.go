package main

// cli_voice_sauc.go — the THREE seams of the v3 SAUC engine on the CLI side.
//
// WHY A THIRD CLOUD ARM. The v2 streaming channel and this one do the same
// WORK (one WebSocket, audio uploaded while it is transcribed, parts of the
// transcript arriving early) with a DIFFERENT protocol, a DIFFERENT product
// line and a DIFFERENT credential shape — and, measured 2026-10-13 on this
// account, the v3 endpoint answers the credential shape the v2 channel cannot
// produce. So this is not a new kind of work; it is the same work on the arm
// the vendor's model 2.0 lives on.
//
// THE ONE FACT THAT SHAPES THE CODE BELOW: the 403. `X-Api-Key: <UUID>` is
// refused by the v3 endpoint ("requested resource not granted") while
// `X-Api-App-Key` + `X-Api-Access-Key` upgrades. That refusal READS like a
// missing entitlement, so it is exactly the report an operator would chase in
// the vendor's console for an hour — which is why the credential's SHAPE is
// decided here, in one resolver, and refused BY NAME before a socket opens
// (voiceSaucDepsReady), rather than discovered as a status code.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/doubaosauc"
	"github.com/nick/gopi/internal/doubaostream"
)

// voiceSaucEngineLabel is the label the HTTP contract (GET /health and the
// /transcribe response) prints for this engine. It is the sibling of
// voiceStreamEngineLabel and is spelled the same way so the two channels of the
// same vendor are distinguishable in one line of `curl /health` output.
const voiceSaucEngineLabel = "doubao-sauc"

// voiceSaucURLSchemePrefix is the only scheme the endpoint may have. It is a
// separate constant from the streaming arm's only because they are separate
// values in separate packages; the CHECK is the same shape (a pre-flight check
// answers "is this a URL at all", never "does it answer").
const voiceSaucURLSchemePrefix = "wss://"

// voiceSaucNoFirstText is the report's sentence when the channel returned
// nothing for the file — the v3 sibling of voiceStreamNoFirstText, quoted by
// name so a test and the message read the same words.
const voiceSaucNoFirstText = "the %s channel produced no transcript for this file"

// The TWO ways the v3 endpoint gets its value, and the only two. They are
// constants rather than inline words because three surfaces quote them — the
// CLI's disclosure, the chat REPL's engine row, and the two notices below — and
// a second spelling at one call site is how two surfaces come to describe
// different origins for one URL (Rule 1/13/14).
const (
	// voiceSaucEndpointSourceSettings is the origin when the value came out of
	// the settings document VERBATIM.
	voiceSaucEndpointSourceSettings = "settings"
	// voiceSaucEndpointSourceEngine is the origin when the block said nothing and
	// the package's own default answered.
	voiceSaucEndpointSourceEngine = "engine"
)

// voiceSaucSettingEndpoint is the endpoint the settings document states, or ""
// when it states none. It is the ONE reader of that knob, so the ORIGIN a notice
// reports and the URL the arm actually opens cannot disagree (Rule 2/13): the
// question "was this configured?" is answered here rather than inferred by each
// caller from a URL it compared against a default.
func voiceSaucSettingEndpoint() string {
	return strings.TrimSpace(voiceSaucSettingSource().Endpoint)
}

// voiceSaucEndpointOrigin reports WHERE an endpoint value came from, in the
// vocabulary above. It exists because the v3 endpoint has TWO producers that both
// look like an ordinary URL at the point of use — and the difference between them
// is a silent, unbounded override.
//
// THE DEFECT IT EXISTS FOR (measured on this host, 2026-10-13): a settings
// document written when the FILE arm was the only arm carried
// audio_engine_param.sauc.endpoint = …/bigmodel_nostream. The LIVE arm's default
// (…/bigmodel_async — the bidirectional endpoint, see voiceSaucLiveEndpoint) was
// added later, and a configured value wins VERBATIM, so the newer default was
// NEVER reached on that host — the fix for live dictation was correct, delivered,
// and silently disabled by one line of old configuration. Nothing said so: the
// run named the endpoint it used and never the reason it had it.
func voiceSaucEndpointOrigin(endpoint string) string {
	if endpoint != "" && endpoint == voiceSaucSettingEndpoint() {
		return voiceSaucEndpointSourceSettings
	}
	return voiceSaucEndpointSourceEngine
}

// voiceSaucEndpointOriginNote is ONE line naming the origin of the endpoint a run
// is about to use, for the operator to read BEFORE the audio moves. With the
// origin named, a URL that no longer matches the arm's contract is a thing the
// operator can find (see the mismatch sentence below) rather than a fact that
// only shows up as "the text arrives all at once, again".
func voiceSaucEndpointOriginNote(endpoint string) string {
	if voiceSaucEndpointOrigin(endpoint) == voiceSaucEndpointSourceSettings {
		return fmt.Sprintf("[voice] sauc endpoint from %s %q (a configured endpoint wins verbatim, and it overrides BOTH arms)",
			voiceSaucEndpointSourceSettings,
			config.AudioParamSettingsPath(config.AudioEngineSauc, config.AudioParamKeyEndpoint))
	}
	return fmt.Sprintf("[voice] sauc endpoint from the %s default (nothing is configured at %s)",
		voiceSaucEndpointSourceEngine,
		config.AudioParamSettingsPath(config.AudioEngineSauc, config.AudioParamKeyEndpoint))
}

// voiceSaucLiveEndpointNote is what a run owes the operator when the endpoint it
// resolved is not the bidirectional one — and "" when it is, or when the endpoint
// is the ENGINE's own default.
//
// THE MISMATCH IS STATED, NEVER CORRECTED. A configured endpoint still wins
// VERBATIM (voiceSaucLiveEndpoint): refusing it, or quietly swapping in the
// default, would take back a decision that is the operator's to make. What must
// not happen is that the decision is INVISIBLE — the live arm exists so the text
// arrives WHILE the operator speaks, and the file-semantics endpoint answers ONCE,
// when the audio has ended, so a live session on it behaves like the recording
// window loop this arm replaced. That is a sentence, not a crash: the run proceeds
// and says what it is doing.
//
// WHY THE ENGINE'S OWN DEFAULT DOES NOT TRIGGER IT, which is the whole reason this
// is not simply "endpoint != bidirectional". The TWO arms have TWO different
// defaults ON PURPOSE (voiceSaucLiveEndpoint): the FILE arm opens the
// file-semantics endpoint and the LIVE arm opens the bidirectional one, and each
// is the correct shape for its own contract. So a file run that took the package
// default is doing exactly what it was designed to do, and a warning there would
// be a false statement about a working configuration — the kind of noise that
// teaches an operator to ignore the row that matters. What IS worth a warning is
// the ONE case that silently breaks an arm: the OPERATOR'S OWN line — written for
// one arm — being applied VERBATIM to both, so the other arm's default becomes
// unreachable (measured 2026-10-13; see voiceSaucEndpointOrigin).
func voiceSaucLiveEndpointNote(endpoint string) string {
	if endpoint == doubaosauc.EndpointBidirectional {
		return ""
	}
	if voiceSaucEndpointOrigin(endpoint) != voiceSaucEndpointSourceSettings {
		return ""
	}
	return fmt.Sprintf("[voice] sauc endpoint %q comes from settings and is not the bidirectional %q: it answers ONCE, "+
		"when the audio has ended. That is what the FILE arm wants and NOT what the LIVE arm wants — there the text would "+
		"appear only after you stopped speaking, which is the recording-window behaviour that arm replaced. A configured "+
		"endpoint wins on BOTH arms (settings %q), so the LIVE arm's own default is unreachable until that line changes",
		endpoint,
		doubaosauc.EndpointBidirectional,
		config.AudioParamSettingsPath(config.AudioEngineSauc, config.AudioParamKeyEndpoint))
}

// voiceSaucEndpointDecisionNote is the ONE place the two sentences above are
// decided: the origin row always (the question "who chose this URL?" always has an
// answer), the mismatch row only when there is one.
func voiceSaucEndpointDecisionNote(endpoint string) []string {
	lines := []string{voiceSaucEndpointOriginNote(endpoint)}
	if note := voiceSaucLiveEndpointNote(endpoint); note != "" {
		lines = append(lines, note)
	}
	return lines
}

// voiceSaucSettingSource resolves this engine's two knobs from the ONE reader
// for the block (config.ResolveAudioSauc), so the CLI's flag rung and the
// package's own default cannot disagree about which endpoint a run used.
func voiceSaucSettingSource() config.AudioSaucParams {
	cfg, err := config.FromSettingsFile(config.ResolveSettingsPath())
	if err != nil || cfg == nil {
		return config.AudioSaucParams{}
	}
	return config.ResolveAudioSauc(cfg)
}

// voiceSaucInlineSettingsLabel is the ONE rendering of "the settings document's
// v3 credential pair" — the string the banner, the start line, the fallback note
// and the help all quote (Rule 13). It is assembled from the block's own path
// helper, so a renamed knob moves every message with it.
func voiceSaucInlineSettingsLabel() string {
	return "settings " + config.AudioParamSettingsPath(config.AudioEngineSauc, config.AudioParamKeyAppID) +
		"/" + config.AudioParamKeyToken
}

// voiceSaucInlineEnvLabel is the same string's environment half.
func voiceSaucInlineEnvLabel() string {
	return "$" + config.AudioAppIDEnv + "/$" + config.AudioTokenEnv
}

// saucCredentialSource is WHERE the v3 credential was found, and what it
// resolves to. It is the ONE resolution point for this channel's credential, so
// the pre-flight check, the privacy notice, the start line and the request
// assembly agree about which rung won (Rule 2).
//
// THE PRECEDENCE, spelled once — and the middle rung is the asymmetry that is
// deliberate rather than accidental:
//
//	a FILE named by an explicit channel (--doubao-key-file >
//	  $GOPI_AUDIO_KEY_FILE / $GOPI_DOUBAO_KEY_FILE >
//	  settings "audio_engine_param.recording.key_file")
//	> the COMPLETE inline pair (settings "audio_engine_param.sauc.app_id" +
//	  ".token", or $GOPI_AUDIO_APP_ID + $GOPI_AUDIO_TOKEN over it)
//	> the LEGACY doubao file ~/.doubao/v2_voice.env — ANNOUNCED, never silent
//
// WHY A NAMED FILE OUTRANKS THE INLINE PAIR: a path is the operator's own
// statement about WHERE the credential lives, and the v2 arm settled the same
// question the same way (voiceDoubaoKeySource: "the stored PATH keeps its own
// rung: it is a named location, and it outranks the inline value"). What the
// inline pair buys is the case that had no answer before — a host where the
// credential is in gopi's own document and the vendor's directory does not
// exist at all.
//
// WHY THE LEGACY FILE IS ANNOUNCED AND NOT DEMOTED TO A REFUSAL: it is the rung
// that makes every pre-existing deployment keep working, and an operator who
// reads "credential: /home/…/.doubao/v2_voice.env" on a run that never
// configured that path learns both that it answered and where the pair now
// belongs (Rule 15 — a fallback nobody was told about is the defect).
type saucCredentialSource struct {
	// inlineAppID / inlineToken are the credential read out of the settings
	// document or its env channel ("" when neither carries one). NEVER printed.
	inlineAppID string
	inlineToken string
	// inlineFrom names the channel the inline pair came from, for the
	// announcement and the refusal.
	inlineFrom string
	// file is the path an EXPLICIT channel named ("" when none did). When it is
	// set, the legacy default is not tried: an operator who pointed the knob at
	// a path means that path.
	file string
	// legacyFile is the terminal fallback path, resolved from $HOME.
	legacyFile string
	// settingsPath is the document the inline pair was read from — the file
	// whose mode the credential guard tightens when it carries a secret.
	settingsPath string
	// modeNote is the sentence to print when the guard HAD to tighten the
	// document's mode, and modeProblem the sentence to print when it could not.
	// Fields rather than a direct write to stderr, so a resolver (which runs
	// several times per invocation) cannot print twice and a test can assert the
	// decision (the same contract voiceDoubaoKeySource carries).
	modeNote    string
	modeProblem string
}

// voiceSaucCredentialSource resolves the v3 credential's location. It DOES NOT
// read a value out of a file (that is resolve/load below), so the pre-flight
// check can answer "is a credential available?" without touching the secret.
//
// Side effect, stated because it is one: when the settings document carries the
// inline pair, its file mode is tightened to 0600 FIRST and the change is
// announced by the caller that prints. Reading a credential out of a
// world-readable document would be the security downgrade the inline rung
// exists to avoid.
func voiceSaucCredentialSource(opts *voiceOptions) saucCredentialSource {
	src := saucCredentialSource{
		settingsPath: config.ResolveSettingsPath(),
		legacyFile:   doubaostream.DefaultCredentialPath(userHomeDirOrDefault()),
	}
	if cfg, err := config.FromSettingsFile(src.settingsPath); err == nil && cfg != nil {
		// THE SETTINGS AND ENV RUNGS COME FROM ONE READER (Rule 2):
		// config.ResolveAudioSauc applies env > nested block. Nothing here
		// re-implements the order or reads the same variable a second time.
		sauc := config.ResolveAudioSauc(cfg)
		src.inlineAppID = strings.TrimSpace(sauc.AppID)
		src.inlineToken = strings.TrimSpace(sauc.Token)
		if src.inlineAppID != "" || src.inlineToken != "" {
			src.inlineFrom = voiceSaucInlineSettingsLabel()
			// The VALUES already carry the environment (ResolveAudioSauc applies
			// it above the block); what the environment changes is only what the
			// message may CLAIM about where they came from — a note that pointed
			// at the settings block while the value came from the shell would
			// send the operator to the wrong file (the same reasoning the v2 arm
			// records beside its own inline rung).
			//
			// THE LABEL IS THE VARIABLE'S NAME, NEVER ITS VALUE (measured in this
			// run: the first cut rendered os.Getenv(name) itself, which would
			// have put a live credential on stderr in the one message that exists
			// to avoid doing exactly that — Rule 15's leak, in a line written to
			// prevent one). Both names are tried because `overEnv` accepts either
			// (the pre-rename spelling is a supported rung), so the label names
			// the one that actually carried the value.
			for _, name := range []string{config.AudioAppIDEnv, config.DoubaoAppIDEnv} {
				if strings.TrimSpace(os.Getenv(name)) != "" {
					src.inlineFrom = "$" + name + "/$" + config.AudioTokenEnv
					break
				}
			}
		}
		// The mode guard runs whether or not THIS call ends up using the inline
		// pair: the document carries a credential either way, and the moment
		// gopi reads the file is the moment it can protect it.
		if note, cerr := config.EnsureSettingsPrivateForCredential(src.settingsPath); cerr != nil {
			// A mode that cannot be tightened is REPORTED, never swallowed
			// (Rule 15).
			src.modeProblem = cerr.Error()
		} else {
			src.modeNote = note
		}
	}
	// The FILE rung, from the ONE key-file resolver the cloud arms share: the
	// flag > the env name > the settings key_file knob, with the pre-rename env
	// name at the same rung. Reusing it is what keeps a host from holding two
	// answers to "where is the credential document".
	if key := voiceDoubaoKeySource(opts); strings.TrimSpace(key.file) != "" {
		src.file = key.file
	}
	return src
}

// halfStated reports the refusal sentence when EXACTLY ONE half of the pair is
// configured, and "" when the pair is whole or entirely absent.
//
// A HALF-STATED PAIR IS REFUSED RATHER THAN COMPLETED FROM THE FILE, and the
// reason is the same class of defect the v3 endpoint's 403 is: the two halves
// are ONE credential, so a stated `app_id` paired with a file's `TOKEN`
// authenticates as NEITHER application — a run that fails with a message about
// the wrong thing. It is refused BY NAME, before any audio moves, and the name
// it carries is the setting to write.
//
// The refusal is NOT a fallback: a half-stated pair does not silently defer to
// the file rung below it (that would be the "complete it from somewhere else"
// behaviour this exists to prevent). It is the operator's own mistake and they
// are told which half is missing.
func (s saucCredentialSource) halfStated() string {
	if (s.inlineAppID == "") == (s.inlineToken == "") {
		return ""
	}
	missing := config.AudioParamKeyAppID
	if s.inlineAppID != "" {
		missing = config.AudioParamKeyToken
	}
	stated := config.AudioParamKeyToken
	if missing == config.AudioParamKeyToken {
		stated = config.AudioParamKeyAppID
	}
	return fmt.Sprintf("the %s credential pair is half-stated: %s is set while %s is not. "+
		"The two halves are ONE credential, and mixing a stated half with another channel's half "+
		"authenticates as neither application — set both (%s, or %s) or neither",
		config.AudioEngineSauc,
		config.AudioParamSettingsPath(config.AudioEngineSauc, stated),
		config.AudioParamSettingsPath(config.AudioEngineSauc, missing),
		voiceSaucInlineSettingsLabel(), voiceSaucInlineEnvLabel())
}

// resolve loads the credential this source resolves to: the pair, the ORIGIN
// label a message may print (a path, or the channel that carried the inline
// pair — NEVER a value), and the error that says why no credential could be
// produced.
//
// The subsystem the file rungs share with the v2 arm is doubaostream's
// KEY=VALUE reader: the v3 pair is the APPID/TOKEN lines of the same document,
// so there is one credential file format on this host, not two.
func (s saucCredentialSource) resolve() (doubaostream.Credential, string, error) {
	if refusal := s.halfStated(); refusal != "" {
		return doubaostream.Credential{}, "", errors.New(refusal)
	}
	if s.file != "" {
		cred, err := doubaostream.LoadCredentials(s.file)
		if err == nil {
			return cred, s.file, nil
		}
		// A file that CANNOT be read does not refuse the run while a complete
		// inline pair is configured: the operator configured both, and the one
		// that works is the one they want used. The substitution is ANNOUNCED
		// (fallbackNote below), never silent — the same decision the v2 arm's
		// streamCredentialSource.load records.
		if s.inlinePairComplete() {
			return s.inlineCredential(), s.inlineFrom, nil
		}
		return doubaostream.Credential{}, s.file, fmt.Errorf(
			"the %s engine needs a credential (%s): %w. %s. %s",
			config.AudioEngineSauc, s.file, err, voiceSaucCredentialShapeHint(), voiceBackToLocalHint())
	}
	if s.inlinePairComplete() {
		return s.inlineCredential(), s.inlineFrom, nil
	}
	// The LEGACY fallback: the vendor's own file, kept working on purpose so an
	// existing deployment does not break because a better location was added.
	cred, err := doubaostream.LoadCredentials(s.legacyFile)
	if err == nil {
		return cred, s.legacyFile, nil
	}
	return doubaostream.Credential{}, s.legacyFile, fmt.Errorf(
		"the %s engine needs a credential (%s): %w. %s. %s",
		config.AudioEngineSauc, s.legacyFile, err, voiceSaucCredentialShapeHint(), voiceBackToLocalHint())
}

// inlinePairComplete reports whether BOTH halves of the inline pair are
// configured. One predicate, so the resolver and the fallback note cannot
// disagree about whether the inline rung answered (Rule 2).
func (s saucCredentialSource) inlinePairComplete() bool {
	return s.inlineAppID != "" && s.inlineToken != ""
}

// inlineCredential is the pair in the transport's vocabulary.
func (s saucCredentialSource) inlineCredential() doubaostream.Credential {
	return doubaostream.Credential{AppID: s.inlineAppID, Token: s.inlineToken}
}

// legacyFileAnswers reports whether the run would end on the LEGACY rung — i.e.
// the doubao file is what this run reads. It is asked by the announcement path
// only, through the same resolution the run itself uses, so a message can never
// describe a route the request would not take.
func (s saucCredentialSource) legacyFileAnswers() bool {
	if s.file != "" || s.inlinePairComplete() {
		return false
	}
	if _, err := os.Stat(s.legacyFile); err != nil {
		return false
	}
	_, err := doubaostream.LoadCredentials(s.legacyFile)
	return err == nil
}

// fallbackNote is the sentence a caller prints WHENEVER this run reads the
// legacy doubao file — the announcement the epic requires — and "" when it does
// not. It names the path that answered and the settings path that replaces it,
// so a reader learns both facts from one line and no value is involved.
func (s saucCredentialSource) fallbackNote() string {
	if !s.legacyFileAnswers() {
		return ""
	}
	return fmt.Sprintf(voiceSaucLegacyFallbackNote, s.legacyFile, voiceSaucInlineSettingsLabel())
}

// voiceSaucCredential is the ONE call the rest of this file makes: the resolved
// credential plus the origin label a message may print.
func voiceSaucCredential(opts *voiceOptions) (doubaostream.Credential, string, error) {
	return voiceSaucCredentialSource(opts).resolve()
}

// voiceSaucCredentialLabel is the ORIGIN of the credential a run would use,
// for a message that must state where it came from before any audio moves.
// It resolves through the same source the request uses (Rule 2) and never
// reads a value: an unresolvable pair reports the path it would have read.
func voiceSaucCredentialLabel(opts *voiceOptions) string {
	src := voiceSaucCredentialSource(opts)
	if _, label, err := src.resolve(); err == nil {
		return label
	}
	return src.legacyFile
}

// voiceReportSaucCredentialMode announces a settings-permission decision on
// stderr. Both sentences name the PATH and the MODES and neither can carry a
// credential value (they are written before the value is read, and they quote
// only the path).
func voiceReportSaucCredentialMode(src saucCredentialSource) {
	if src.modeProblem != "" {
		fmt.Fprintf(os.Stderr, "[gopi] %s\n", src.modeProblem)
		return
	}
	if src.modeNote != "" {
		fmt.Fprintf(os.Stderr, "[gopi] %s\n", src.modeNote)
	}
}

// voiceSaucEffectiveEndpoint is the URL a run will actually open, after the
// block and the package default. It is spelled once here because THREE callers
// state it (the privacy notice, the deps check's scheme test, the request) and
// a second spelling is how two of them come to describe different destinations.
func voiceSaucEffectiveEndpoint() string {
	if ep := strings.TrimSpace(voiceSaucSettingSource().Endpoint); ep != "" {
		return ep
	}
	return doubaosauc.DefaultEndpoint
}

// voiceSaucEffectiveResourceID is the product line the request is billed
// against, resolved the same ONE way.
func voiceSaucEffectiveResourceID() string {
	if rid := strings.TrimSpace(voiceSaucSettingSource().ResourceID); rid != "" {
		return rid
	}
	return doubaosauc.DefaultResourceID
}

// voiceSaucOpts assembles the request from the resolved knobs. It reads the
// credential at the ONE moment the request is built and hands the VALUES
// straight into doubaosauc.Options: they are never printed, logged, or written
// to a report (the report carries the ORIGIN, and only when a run needs to say
// where it looked).
func voiceSaucOpts(opts *voiceOptions) (doubaosauc.Options, string, error) {
	src := voiceSaucCredentialSource(opts)
	cred, label, err := src.resolve()
	if err != nil {
		return doubaosauc.Options{}, label, err
	}
	// The mode decision belongs here as much as in the pre-flight check: the
	// pre-flight runs FIRST and its note is printed, but a caller that reaches
	// the request without one (the live arm, a server arming) must not handle a
	// credential out of a world-readable document in silence (Rule 15).
	voiceReportSaucCredentialMode(src)
	// CredentialFile is set only when the credential CAME from a file: the
	// package uses it to name a path in a message, and handing it the inline
	// channel's label would make that message claim a file nobody read. When the
	// pair is inline this is empty, which the package reads as "the caller stated
	// the pair itself" — and this caller always does, via AppKey/AccessKey.
	credentialFile := src.file
	if credentialFile == "" && !src.inlinePairComplete() {
		credentialFile = src.legacyFile
	}
	return doubaosauc.Options{
		AppKey:         cred.AppID,
		AccessKey:      cred.Token,
		CredentialFile: credentialFile,
		Endpoint:       voiceSaucEffectiveEndpoint(),
		ResourceID:     voiceSaucEffectiveResourceID(),
		AudioFile:      opts.audio,
	}, label, nil
}

// voiceSaucLiveEndpoint is the URL the LIVE arm of this engine opens, and it is
// THE ONE PLACE that decision is made.
//
// THE DEFAULT IS THE BIDIRECTIONAL ENDPOINT (`bigmodel_async`), and it is not the
// same default as the FILE arm's for a reason that is about the WORK rather than
// about taste: this arm exists so the text appears WHILE the operator speaks, and
// the file-semantics endpoint (`bigmodel_nostream`) answers once, at the end — a
// live session on it would show nothing until the utterance was over, which is
// the window loop's behaviour this arm replaces. The two defaults are therefore
// both correct and they are NOT contradictory: each arm's default is the endpoint
// its own contract needs.
//
// A CONFIGURED ENDPOINT WINS, and it wins VERBATIM — including a file-semantics
// one, because that is a decision the operator is entitled to make. What must not
// happen is the opposite: a file arm silently inheriting this arm's default, which
// is why the file arm keeps calling voiceSaucEffectiveEndpoint below.
func voiceSaucLiveEndpoint() string {
	if ep := strings.TrimSpace(voiceSaucSettingSource().Endpoint); ep != "" {
		return ep
	}
	return doubaosauc.EndpointBidirectional
}

// voiceSaucLiveOpts builds the LIVE arm's session options. It is voiceSaucOpts'
// sibling and it differs in exactly two things: the endpoint (above) and the fact
// that the audio is NOT a file — a live session is fed by Say, so AudioFile is
// left empty on purpose rather than filled with something the session would
// ignore.
func voiceSaucLiveOpts(opts *voiceOptions) (doubaosauc.Options, string, error) {
	o, path, err := voiceSaucOpts(opts)
	if err != nil {
		return doubaosauc.Options{}, path, err
	}
	o.Endpoint = voiceSaucLiveEndpoint()
	o.AudioFile = ""
	return o, path, nil
}

// voiceSaucDepsReady is the pre-flight check: can this run proceed at all?
//
// IT ANSWERS BEFORE ANY AUDIO MOVES, and it answers in the same order the
// request would fail in — the credential's SHAPE first (the refusal that looks
// like an entitlement problem), then the credential's CONTENT, then the
// endpoint's shape. A well-formed but unreachable host is the network's report,
// not this one's.
func voiceSaucDepsReady(opts *voiceOptions) string {
	src := voiceSaucCredentialSource(opts)
	// The mode decision is REPORTED from here as well as from the request
	// builder, and that is deliberate: this is the first function a real run
	// reaches, so a note that only the request path printed would arrive after
	// the operator had already been told the run was fine (Rule 15).
	voiceReportSaucCredentialMode(src)
	if _, _, err := src.resolve(); err != nil {
		// The refusal is the resolver's own sentence (Rule 15): it names the
		// half that is missing, or the path that could not be read, and never a
		// value.
		return err.Error()
	}
	if note := src.substitutedNote(); note != "" {
		// ANNOUNCED, not refused: a named file that could not be read while a
		// complete inline pair answered is a fact the operator must learn at the
		// moment it happens (Rule 15 — the same decision the v2 arm makes).
		fmt.Fprintf(os.Stderr, "[gopi] %s\n", note)
	}
	if path := src.filePathForMode(); path != "" {
		if note := doubaosauc.CredentialFileModeNote(path); note != "" {
			// ANNOUNCED, not refused: a world-readable credential is a fact the
			// operator must learn at the moment it is used, and it does not stop
			// this run (the same decision the v2 arm makes).
			fmt.Fprintf(os.Stderr, "[gopi] %s\n", note)
		}
	}
	if ep := voiceSaucEffectiveEndpoint(); !strings.HasPrefix(ep, voiceSaucURLSchemePrefix) {
		return fmt.Sprintf("the %s engine needs a %s endpoint, and %q is not one (settings %q)",
			config.AudioEngineSauc, voiceSaucURLSchemePrefix, ep,
			config.AudioParamSettingsPath(config.AudioEngineSauc, config.AudioParamKeyEndpoint))
	}
	return ""
}

// filePathForMode is the credential FILE this run reads, or "" when the pair is
// inline (there is no file, and inventing a path would mode-check a document
// that is not the credential).
func (s saucCredentialSource) filePathForMode() string {
	if s.file != "" {
		if _, err := doubaostream.LoadCredentials(s.file); err == nil {
			return s.file
		}
		return ""
	}
	if s.inlinePairComplete() {
		return ""
	}
	if _, err := doubaostream.LoadCredentials(s.legacyFile); err == nil {
		return s.legacyFile
	}
	return ""
}

// substitutedNote reports the sentence a caller must print when an explicitly
// named FILE was tried and could not be used while the inline pair answered
// instead, and "" when nothing was substituted. It re-runs the same resolution
// rather than caching state, so the pre-flight check and the run cannot disagree
// about whether a substitution happened (Rule 2).
func (s saucCredentialSource) substitutedNote() string {
	if s.file == "" || !s.inlinePairComplete() {
		return ""
	}
	if _, err := doubaostream.LoadCredentials(s.file); err == nil {
		return ""
	}
	return fmt.Sprintf(voiceSaucCredentialFileFellThroughNote, s.file, s.inlineFrom)
}

// voiceSaucCredentialShapeHint is the sentence that turns the vendor's 403 into
// a one-line fix. It is written once (Rule 13) because three surfaces owe it:
// the pre-flight refusal above, the privacy notice, and the report.
func voiceSaucCredentialShapeHint() string {
	return fmt.Sprintf("the v3 endpoint takes the LEGACY CONSOLE PAIR — %s + %s — carried as the "+
		"APPID and TOKEN lines of a KEY=VALUE file; the single-key shape answers HTTP 403 "+
		"\"requested resource not granted\", which reads like a missing entitlement and is not one",
		doubaosauc.HeaderAppKey, doubaosauc.HeaderAccessKey)
}

// voiceSaucLegacyFallbackNote is the ANNOUNCEMENT a run owes when the vendor's
// own file is what answered (Rule 15: a fallback nobody was told about is the
// defect). It names the path that answered, the settings path that supersedes
// it, and — because a message may say nothing else about a credential — neither
// half of the pair.
//
// `%s` is the path, `%s` is the settings path; BOTH are paths, so the sentence
// cannot carry a value even by accident.
const voiceSaucLegacyFallbackNote = "[voice] sauc credential: falling back to the legacy doubao file %s " +
	"— this pair now belongs in gopi's own settings document (%s), which outranks it"

// voiceSaucCredentialFileFellThroughNote is the same announcement for the other
// substitution: a file an explicit channel NAMED could not be read and the
// inline pair answered instead. `%s` is the path, `%s` the channel that
// supplied the pair.
const voiceSaucCredentialFileFellThroughNote = "[voice] sauc credential: the file %s could not be read; " +
	"using %s instead"

// voiceSaucPrivacyNotice is this engine's upload disclosure: the same four
// facts the batch engine's notice states (what is sent, where it goes, whether
// anything chose it, how to take it back), for the same reason — an engine that
// uploads audio must say so at the moment it commits, not leave it to be
// inferred.
//
// It names the credential's SHAPE and its ORIGIN — a path, or the settings/env
// channel that carries the pair — and never a value, because on this channel the
// shape is the difference between a working run and a 403.
func voiceSaucPrivacyNotice(opts *voiceOptions) string {
	who := "chosen on one of the three channels (flag, env or settings)"
	return fmt.Sprintf(
		"[voice] PRIVACY — engine %s UPLOADS the audio to %s (X-Api-Resource-Id %s); the recording leaves this machine (%s). "+
			"credential: %s. %s",
		config.AudioEngineSauc,
		voiceSaucEffectiveEndpoint(),
		voiceSaucEffectiveResourceID(),
		who,
		voiceSaucCredentialLabel(opts),
		voiceBackToLocalHint())
}

// voiceReportSaucPrivacy writes the notice above to stderr, through the same
// shape as the batch engine's. It is called by the process that UPLOADS (the
// one-shot run, the server arming, the capture path) and never by the capture
// client, which transcribes nothing.
func voiceReportSaucPrivacy(opts *voiceOptions) {
	fmt.Fprintf(os.Stderr, "%s\n", voiceSaucPrivacyNotice(opts))
	// WHO CHOSE THE DESTINATION, immediately after the destination itself — the
	// one sentence this whole change is about. The v3 endpoint has two producers
	// and the settings one wins VERBATIM on BOTH arms, so a line written for the
	// FILE arm silently disables the LIVE arm's default (`bigmodel_async`), which
	// is a fix that was delivered and never ran (measured 2026-10-13, task
	// 01a13303). Announced, never enforced: the run proceeds and uses exactly the
	// endpoint the operator configured — it simply says so, and says what a live
	// session on that endpoint will do.
	//
	// IT RIDES THE DISCLOSURE THAT ALREADY EXISTS rather than a new call site:
	// this is the function every sauc run that UPLOADS already goes through
	// (needsEngine), so there is one announcement, before the audio moves, and no
	// second place to keep in step (Rule 2/3).
	for _, line := range voiceSaucEndpointDecisionNote(voiceSaucEffectiveEndpoint()) {
		fmt.Fprintf(os.Stderr, "%s\n", line)
	}
}

// transcribeFileSauc is the file shape of this engine, and it is the ONE seam
// in this file that does the work: it hands the path and the resolved knobs to
// the package and turns the package's result into the CLI's two figures.
//
// THE FIRST TEXT OF THIS ARM IS ITS ANSWER, like the batch and local arms: the
// default endpoint is the FILE-semantics one (bigmodel_nostream), which returns
// one final transcript for the whole recording. So the report is built by the
// SAME helper those two use (voiceFirstTextFromEngine), and the two numbers it
// carries stay comparable across engines instead of being produced by a third
// formula.
func transcribeFileSauc(opts *voiceOptions, audioPath string, runClock *voiceRunClock) (string, time.Duration, voiceFirstTextReport, error) {
	var report voiceFirstTextReport
	src := voiceSaucCredentialSource(opts)
	saucOpts, path, err := voiceSaucOpts(opts)
	if err != nil {
		return "", 0, report, fmt.Errorf("the %s engine needs a credential (%s): %w",
			config.AudioEngineSauc, path, err)
	}
	saucOpts.AudioFile = audioPath

	// The endpoint, the credential's ORIGIN (never its value) and the ONE
	// announcement this run owes — the legacy fallback, when the vendor's file
	// is what answered — are stated once, before the audio moves: the same
	// disclosure every other uploading shape prints.
	if note := src.fallbackNote(); note != "" {
		fmt.Fprintf(os.Stderr, "%s\n", note)
	}
	voiceReportSaucStart(path)

	started := time.Now()
	res, err := doubaosauc.Transcribe(context.Background(), saucOpts)
	elapsed := time.Since(started)
	if err != nil {
		return "", elapsed, report, voiceSaucError(err)
	}
	voiceReportSaucSummary(res)
	return res.Text, elapsed, voiceFirstTextFromEngine(elapsed, runClock), nil
}

// voiceReportSaucStart states the destination and the credential's ORIGIN
// before the audio moves. Neither line can carry a value: `origin` is a path or
// a settings/env channel, which is why the word is `credential` and not
// `credential file` (an inline pair has no file, and a line that promised one
// would describe a document nobody read).
func voiceReportSaucStart(origin string) {
	fmt.Fprintf(os.Stderr, "[voice] %s channel → %s (resource %s), credential %s\n",
		config.AudioEngineSauc, voiceSaucEffectiveEndpoint(), voiceSaucEffectiveResourceID(), origin)
}

// voiceReportSaucSummary prints what the CHANNEL observed — the vendor's log id
// (the ONE identifier the vendor's support asks for), how much audio went out,
// in how many packets, and when the text first existed. It is the v3 sibling of
// voiceReportStreamSummary and exists for the same reason: a run whose figures
// are only in a report cannot be compared with the next run.
func voiceReportSaucSummary(res *doubaosauc.Result) {
	if res == nil {
		return
	}
	fmt.Fprintf(os.Stderr,
		"[voice] %s: %d chars, %d ms audio, %d packets, first text %s, logid %s\n",
		config.AudioEngineSauc, len([]rune(res.Text)), res.AudioMS, res.Packets,
		res.FirstTextAt.Round(time.Millisecond), res.LogID)
}

// voiceSaucError turns the package's failure into the CLI's sentence. The
// package already carries the step, the code, the service's wording and the
// vendor's log id; this function's whole job is to keep that verbatim (Rule 15)
// and to say the one thing only the CLI knows: which FILE the credential came
// from, when the failure was the credential.
func voiceSaucError(err error) error {
	var noSpeech *doubaosauc.NoSpeechError
	if errors.As(err, &noSpeech) {
		// NOT a failure of the engine: the audio carries no speech, and saying
		// so is the true sentence. It is an error return so no caller can print
		// "" as though it were a recognition result.
		return noSpeech
	}
	return fmt.Errorf("%s engine: %w", config.AudioEngineSauc, err)
}
